package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeWebSearcher is a test double for api.WebSearcher.
type fakeWebSearcher struct {
	results []api.WebSearchResult
	err     error
	calls   []api.WebSearchOptions
}

func (f *fakeWebSearcher) Search(_ context.Context, query string, opts api.WebSearchOptions) ([]api.WebSearchResult, error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return nil, f.err
	}
	var out []api.WebSearchResult
	for _, r := range f.results {
		if query == "" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func TestNewHTTPWebSearcher_RequiresEndpoint(t *testing.T) {
	t.Parallel()
	_, err := NewHTTPWebSearcher("", "", nil, 0)
	if !errors.Is(err, ErrWebSearchNotConfigured) {
		t.Fatalf("expected ErrWebSearchNotConfigured, got: %v", err)
	}
}

func TestNewHTTPWebSearcher_InvalidURL(t *testing.T) {
	t.Parallel()
	_, err := NewHTTPWebSearcher("http://[::1]:namedport", "", nil, 0)
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestNewHTTPWebSearcher_InvalidScheme(t *testing.T) {
	t.Parallel()
	_, err := NewHTTPWebSearcher("ftp://example.com", "", nil, 0)
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}
}

func TestHTTPWebSearcher_Search(t *testing.T) {
	t.Parallel()
	results := []api.WebSearchResult{
		{Title: "Go", URL: "https://go.dev", Snippet: "The Go programming language", Date: "2024-01-01"},
		{Title: "Go Wiki", URL: "https://github.com/golang/go/wiki", Snippet: "Wiki", Content: "body"},
	}

	var receivedQuery string
	var receivedLimit string
	var receivedInclude string
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("q")
		receivedLimit = r.URL.Query().Get("limit")
		receivedInclude = r.URL.Query().Get("include_content")
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "secret", testFetchClient(server), 5*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}

	ctx := context.Background()
	got, err := searcher.Search(ctx, "golang", api.WebSearchOptions{Limit: 3, IncludeContent: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != len(results) {
		t.Fatalf("expected %d results, got %d", len(results), len(got))
	}
	if receivedQuery != "golang" {
		t.Errorf("query = %q, want %q", receivedQuery, "golang")
	}
	if receivedLimit != "3" {
		t.Errorf("limit = %q, want 3", receivedLimit)
	}
	if receivedInclude != "true" {
		t.Errorf("include_content = %q, want true", receivedInclude)
	}
	if authHeader != "Bearer secret" {
		t.Errorf("authorization = %q, want Bearer secret", authHeader)
	}
}

func TestHTTPWebSearcher_Search_DefaultsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		if limit != "5" {
			t.Errorf("expected default limit 5, got %q", limit)
		}
		_ = json.NewEncoder(w).Encode([]api.WebSearchResult{})
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestHTTPWebSearcher_Search_CapsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		if limit != "20" {
			t.Errorf("expected capped limit 20, got %q", limit)
		}
		_ = json.NewEncoder(w).Encode([]api.WebSearchResult{})
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{Limit: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestHTTPWebSearcher_Search_EmptyQuery(t *testing.T) {
	t.Parallel()
	searcher, err := NewHTTPWebSearcher("http://example.com", "", nil, 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestHTTPWebSearcher_Search_NonOK(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "boom")
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestHTTPWebSearcher_Search_InvalidJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewHTTPWebSearcher_DoesNotMutateCallerClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.WebSearchResult{})
	}))
	defer server.Close()

	original := &http.Client{Timeout: 1 * time.Second}
	_, err := NewHTTPWebSearcher("http://example.com", "", original, 5*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	if original.Timeout != 1*time.Second {
		t.Errorf("caller client timeout was mutated to %v", original.Timeout)
	}
}

func TestHTTPWebSearcher_Search_RejectsNull(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "null")
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error for null results")
	}
}

func TestHTTPWebSearcher_Search_ReadBodyError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "{")
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	_, err = searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error reading response body")
	}
}

func TestHTTPWebSearcher_Search_ContextCancel(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]api.WebSearchResult{})
	}))
	defer server.Close()

	searcher, err := NewHTTPWebSearcher("http://example.com", "", testFetchClient(server), 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = searcher.Search(ctx, "test", api.WebSearchOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHTTPWebSearcher_Search_NilReceiver(t *testing.T) {
	t.Parallel()
	var searcher *HTTPWebSearcher
	_, err := searcher.Search(context.Background(), "test", api.WebSearchOptions{})
	if !errors.Is(err, ErrWebSearchNotConfigured) {
		t.Fatalf("expected ErrWebSearchNotConfigured, got: %v", err)
	}
}

func TestHTTPWebSearcher_NilClientUsesSecureClient(t *testing.T) {
	t.Parallel()
	searcher, err := NewHTTPWebSearcher("http://example.com", "", nil, 0)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	if searcher.client == nil {
		t.Fatal("expected default client to be set")
	}
}

func TestHTTPWebSearcher_NilClientAppliesTimeout(t *testing.T) {
	t.Parallel()
	searcher, err := NewHTTPWebSearcher("http://example.com", "", nil, 5*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPWebSearcher: %v", err)
	}
	if searcher.client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", searcher.client.Timeout)
	}
}
