package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
)

const maxHTTPResponseSize = 8 * 1024 * 1024 // 8 MB

// HTTPTransport implements Transport using JSON-RPC POST requests over HTTP.
// It is a minimal implementation suitable for MCP servers that accept a single
// JSON-RPC request per HTTP POST and return the JSON-RPC response in the body.
type HTTPTransport struct {
	url        string
	headers    map[string]string
	bearerEnv  string
	client     *http.Client
	ownsClient bool
	nextID     int64
}

// NewHTTPTransport creates a new HTTP MCP transport.
func NewHTTPTransport(url string, headers map[string]string, bearerEnv string, client *http.Client) *HTTPTransport {
	ownsClient := false
	if client == nil {
		client = netutil.SecureHTTPClient()
		ownsClient = true
	}
	return &HTTPTransport{
		url:        url,
		headers:    headers,
		bearerEnv:  bearerEnv,
		client:     client,
		ownsClient: ownsClient,
	}
}

// Connect is a no-op for the HTTP transport; connections are created per request.
func (t *HTTPTransport) Connect(_ context.Context) error {
	return nil
}

// Send sends a JSON-RPC request via HTTP POST and returns the response.
func (t *HTTPTransport) Send(ctx context.Context, method string, params any) (resp *JSONRPCResponse, err error) {
	id := atomic.AddInt64(&t.nextID, 1)
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.bearerEnv != "" {
		if token := os.Getenv(t.bearerEnv); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	httpResp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", method, err)
	}
	defer func() {
		if cerr := httpResp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close response body for %s: %w", method, cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxHTTPResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read response body for %s: %w", method, err)
	}
	if len(body) > maxHTTPResponseSize {
		return nil, fmt.Errorf("response body for %s exceeds maximum allowed size (%d bytes)", method, maxHTTPResponseSize)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, maxHTTPResponseSize+1))
		return nil, fmt.Errorf("http %d for %s: %s", httpResp.StatusCode, method, string(body))
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response for %s: %w (body: %s)", method, err, string(body))
	}
	if resp.Error != nil {
		return resp, resp.Error
	}
	return resp, nil
}

// Notify sends a JSON-RPC notification via HTTP POST.
func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) (err error) {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.bearerEnv != "" {
		if token := os.Getenv(t.bearerEnv); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	httpResp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("post notification %s: %w", method, err)
	}
	defer func() {
		if cerr := httpResp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close notification body for %s: %w", method, cerr)
		}
	}()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, maxHTTPResponseSize+1))
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, maxHTTPResponseSize+1))
		return fmt.Errorf("http %d for notification %s: %s", httpResp.StatusCode, method, string(body))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, maxHTTPResponseSize+1))
	return nil
}

// Close closes idle connections if the transport created its own HTTP client.
func (t *HTTPTransport) Close() error {
	if t.ownsClient && t.client != nil {
		t.client.CloseIdleConnections()
	}
	return nil
}
