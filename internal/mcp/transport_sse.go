package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
)

const (
	maxSSEResponseSize = 8 * 1024 * 1024 // 8 MB
	maxSSELineSize     = 256 * 1024      // 256 KB per line
)

// SSETransport implements the MCP Transport interface over Server-Sent Events.
// It connects to an SSE endpoint, discovers the JSON-RPC POST endpoint, and
// routes requests/responses through the event stream.
type SSETransport struct {
	url       string
	headers   map[string]string
	bearerEnv string
	client    *http.Client

	mu        sync.Mutex
	connectMu sync.Mutex
	postURL   string
	pending   map[int64]chan *JSONRPCResponse
	nextID    int64
	ctx       context.Context
	cancel    context.CancelFunc
	readErr   error
	respBody  io.ReadCloser
	closed    bool
}

// NewSSETransport creates a new SSE MCP transport.
func NewSSETransport(serverURL string, headers map[string]string, bearerEnv string, client *http.Client) *SSETransport {
	if client == nil {
		client = netutil.SecureHTTPClient()
	}
	return &SSETransport{
		url:       serverURL,
		headers:   headers,
		bearerEnv: bearerEnv,
		client:    client,
		pending:   make(map[int64]chan *JSONRPCResponse),
	}
}

// Connect establishes the SSE connection and discovers the POST endpoint.
func (t *SSETransport) Connect(ctx context.Context) error {
	t.connectMu.Lock()
	defer t.connectMu.Unlock()

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("sse transport is closed")
	}
	if t.ctx != nil {
		t.mu.Unlock()
		return fmt.Errorf("sse transport already connected")
	}
	t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
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
		return fmt.Errorf("connect sse: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, maxSSEResponseSize+1))
		_ = httpResp.Body.Close()
		return fmt.Errorf("sse connect http %d: %s", httpResp.StatusCode, string(body))
	}

	// Read until we discover the endpoint event.
	reader := bufio.NewReaderSize(httpResp.Body, maxSSELineSize)
	endpoint, err := t.readEndpoint(reader)
	if err != nil {
		_ = httpResp.Body.Close()
		return fmt.Errorf("discover sse endpoint: %w", err)
	}

	postURL, err := resolveReference(t.url, endpoint)
	if err != nil {
		_ = httpResp.Body.Close()
		return fmt.Errorf("resolve sse endpoint: %w", err)
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = httpResp.Body.Close()
		return fmt.Errorf("transport closed while connecting")
	}
	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.respBody = httpResp.Body
	t.postURL = postURL
	t.mu.Unlock()

	go t.readLoop(reader)
	return nil
}

// readEndpoint reads SSE events until an "endpoint" event is found.
func (t *SSETransport) readEndpoint(reader *bufio.Reader) (string, error) {
	for {
		ev, err := readSSEEvent(reader)
		if err != nil {
			return "", err
		}
		if ev.event == "endpoint" {
			return ev.data, nil
		}
	}
}

// readLoop continuously reads SSE events and dispatches JSON-RPC responses.
func (t *SSETransport) readLoop(reader *bufio.Reader) {
	defer func() { _ = t.Close() }()

	for {
		ev, err := readSSEEvent(reader)
		if err != nil {
			t.setReadErr(err)
			return
		}
		if ev.event != "message" || ev.data == "" {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(ev.data), &resp); err != nil {
			t.setReadErr(fmt.Errorf("unmarshal sse message: %w", err))
			return
		}

		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.mu.Unlock()

		if ok {
			select {
			case ch <- &resp:
			case <-t.ctx.Done():
				return
			}
		}
	}
}

// setReadErr stores the first read error and cancels pending requests.
func (t *SSETransport) setReadErr(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.readErr != nil {
		return
	}
	t.readErr = err
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
}

// Send sends a JSON-RPC request via POST and waits for the SSE response.
func (t *SSETransport) Send(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	id := atomic.AddInt64(&t.nextID, 1)
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	respCh := make(chan *JSONRPCResponse, 1)
	t.mu.Lock()
	if t.readErr != nil {
		t.mu.Unlock()
		return nil, t.readErr
	}
	t.pending[id] = respCh
	postURL := t.postURL
	t.mu.Unlock()

	if err := t.post(ctx, postURL, reqBody); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("send cancelled: %w", ctx.Err())
	case <-t.ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		err := t.readErr
		t.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("transport closed")
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("transport closed")
		}
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	}
}

// Notify sends a JSON-RPC notification via POST.
func (t *SSETransport) Notify(ctx context.Context, method string, params any) error {
	reqBody := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	t.mu.Lock()
	postURL := t.postURL
	t.mu.Unlock()

	return t.post(ctx, postURL, reqBody)
}

// post sends a JSON body to the given URL.
func (t *SSETransport) post(ctx context.Context, postURL string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
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
		return fmt.Errorf("post: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, maxSSEResponseSize+1))
		return fmt.Errorf("http %d: %s", httpResp.StatusCode, string(respBody))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, maxSSEResponseSize+1))
	return nil
}

// Close cancels the SSE connection and cleans up resources.
func (t *SSETransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	cancel := t.cancel
	body := t.respBody
	t.cancel = nil
	t.respBody = nil
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if body != nil {
		_ = body.Close()
	}
	return nil
}

// sseEvent represents a single Server-Sent Event.
type sseEvent struct {
	event string
	data  string
}

// readSSEEvent reads one SSE event from the buffered reader.
func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return ev, fmt.Errorf("read sse line: %w", err)
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		if line == "" {
			// End of event.
			ev.data = strings.Join(dataLines, "\n")
			return ev, nil
		}

		if !strings.Contains(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			ev.event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
}

// resolveReference resolves endpoint against baseURL. endpoint may be absolute
// or relative.
func resolveReference(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint url: %w", err)
	}
	return base.ResolveReference(ref).String(), nil
}
