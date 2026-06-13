package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPTransport_RoundTrip(t *testing.T) {
	t.Parallel()

	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Method == "initialize" {
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
			})
			return
		}
		if req.Method == "tools/list" {
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"tools":[{"name":"read_file","description":"Read"}]}`),
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	tr := NewHTTPTransport(server.URL, nil, "", server.Client())
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	resp, err := tr.Send(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	})
	if err != nil {
		t.Fatalf("Send initialize error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned JSON-RPC error: %v", resp.Error)
	}

	resp2, err := tr.Send(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Send tools/list error: %v", err)
	}
	if resp2.Error != nil {
		t.Fatalf("tools/list returned JSON-RPC error: %v", resp2.Error)
	}

	if atomic.LoadInt64(&requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestHTTPTransport_HeadersAndBearerToken(t *testing.T) {
	t.Setenv("MCP_TEST_TOKEN", "secret-token")

	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{}`),
		})
	}))
	defer server.Close()

	tr := NewHTTPTransport(
		server.URL,
		map[string]string{"X-Custom": "value"},
		"MCP_TEST_TOKEN",
		server.Client(),
	)
	if _, err := tr.Send(context.Background(), "ping", nil); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if gotHeaders.Get("X-Custom") != "value" {
		t.Errorf("X-Custom header = %q, want value", gotHeaders.Get("X-Custom"))
	}
	if auth := gotHeaders.Get("Authorization"); auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", auth)
	}
}

func TestHTTPTransport_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
	}))
	defer server.Close()

	tr := NewHTTPTransport(server.URL, nil, "", server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Send(ctx, "slow", nil)
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestHTTPTransport_Non2xxStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	tr := NewHTTPTransport(server.URL, nil, "", server.Client())
	_, err := tr.Send(context.Background(), "bad", nil)
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected HTTP 400 error, got: %v", err)
	}
}

func TestHTTPTransport_Notify(t *testing.T) {
	t.Parallel()

	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	tr := NewHTTPTransport(server.URL, nil, "", server.Client())
	if err := tr.Notify(context.Background(), "notifications/initialized", nil); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if !called {
		t.Fatal("expected server to receive notification")
	}
}

func TestHTTPTransport_DefaultClient(t *testing.T) {
	t.Parallel()

	tr := NewHTTPTransport("http://example.com", nil, "", nil)
	if tr.client == nil {
		t.Fatal("expected default HTTP client")
	}
}

func TestHTTPTransport_BearerTokenMissingEnv(t *testing.T) {
	t.Setenv("MCP_MISSING_TOKEN", "")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
	}))
	defer server.Close()

	tr := NewHTTPTransport(server.URL, nil, "MCP_MISSING_TOKEN", server.Client())
	if _, err := tr.Send(context.Background(), "ping", nil); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty", gotAuth)
	}
}
