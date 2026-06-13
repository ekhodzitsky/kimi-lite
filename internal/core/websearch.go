package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ErrWebSearchNotConfigured is returned when a web search is attempted but no
// provider is configured.
var ErrWebSearchNotConfigured = errors.New("web search provider is not configured")

// HTTPWebSearcher performs web searches against a configurable HTTP JSON API.
type HTTPWebSearcher struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewHTTPWebSearcher creates a web searcher that calls endpoint.
// If client is nil, a secure default client is used.
func NewHTTPWebSearcher(endpoint, apiKey string, client *http.Client, timeout time.Duration) (*HTTPWebSearcher, error) {
	if endpoint == "" {
		return nil, ErrWebSearchNotConfigured
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid web search endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid web search endpoint scheme: %q", u.Scheme)
	}
	if client == nil {
		client = netutil.SecureHTTPClient()
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return &HTTPWebSearcher{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   client,
	}, nil
}

// Search calls the configured endpoint with query parameters and returns results.
// The endpoint is expected to return a JSON array of api.WebSearchResult.
func (s *HTTPWebSearcher) Search(ctx context.Context, query string, opts api.WebSearchOptions) ([]api.WebSearchResult, error) {
	if s == nil || s.endpoint == "" {
		return nil, ErrWebSearchNotConfigured
	}
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	if opts.Limit > 20 {
		opts.Limit = 20
	}

	u, err := url.Parse(s.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(opts.Limit))
	q.Set("include_content", strconv.FormatBool(opts.IncludeContent))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read web search response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search returned status %d: %s", resp.StatusCode, string(body))
	}

	var results []api.WebSearchResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode web search response: %w", err)
	}
	return results, nil
}
