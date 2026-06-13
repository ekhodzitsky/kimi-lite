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
	"time"
)

// HTTPTransport implements Transport using JSON-RPC POST requests over HTTP.
// It is a minimal implementation suitable for MCP servers that accept a single
// JSON-RPC request per HTTP POST and return the JSON-RPC response in the body.
type HTTPTransport struct {
	url       string
	headers   map[string]string
	bearerEnv string
	client    *http.Client
	nextID    int64
}

// NewHTTPTransport creates a new HTTP MCP transport.
func NewHTTPTransport(url string, headers map[string]string, bearerEnv string, client *http.Client) *HTTPTransport {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPTransport{
		url:       url,
		headers:   headers,
		bearerEnv: bearerEnv,
		client:    client,
	}
}

// Connect is a no-op for the HTTP transport; connections are created per request.
func (t *HTTPTransport) Connect(ctx context.Context) error {
	return nil
}

// Send sends a JSON-RPC request via HTTP POST and returns the response.
func (t *HTTPTransport) Send(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
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

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body for %s: %w", method, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d for %s: %s", resp.StatusCode, method, string(body))
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response for %s: %w (body: %s)", method, err, string(body))
	}
	if rpcResp.Error != nil {
		return &rpcResp, rpcResp.Error
	}
	return &rpcResp, nil
}

// Notify sends a JSON-RPC notification via HTTP POST.
func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) error {
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

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("post notification %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d for notification %s: %s", resp.StatusCode, method, string(body))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Close is a no-op for the HTTP transport.
func (t *HTTPTransport) Close() error {
	return nil
}
