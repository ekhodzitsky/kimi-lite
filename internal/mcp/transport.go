// Package mcp provides an MCP client implementation using JSON-RPC over stdio.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Transport defines the interface for MCP JSON-RPC transports.
type Transport interface {
	// Connect establishes the underlying connection.
	Connect(ctx context.Context) error
	// Send sends a JSON-RPC request and waits for a matching response.
	Send(ctx context.Context, method string, params interface{}) (*JSONRPCResponse, error)
	// Notify sends a JSON-RPC notification (no response expected).
	Notify(ctx context.Context, method string, params interface{}) error
	// Close gracefully shuts down the transport.
	Close() error
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *JSONRPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// StdioTransport implements Transport using stdin/stdout of a subprocess.
type StdioTransport struct {
	command string
	args    []string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	pending map[int64]chan *JSONRPCResponse
	nextID  int64
	closed  bool
	wg      sync.WaitGroup
	readErr error
	newCmd  func(name string, arg ...string) *exec.Cmd
	writeMu sync.Mutex
}

// NewStdioTransport creates a new stdio transport for the given command.
func NewStdioTransport(command string, args ...string) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		newCmd:  exec.Command,
	}
}

// Connect starts the subprocess and begins reading responses.
func (t *StdioTransport) Connect(ctx context.Context) error {
	t.mu.Lock()

	if t.cmd != nil {
		t.mu.Unlock()
		return fmt.Errorf("transport already connected")
	}

	path, err := exec.LookPath(t.command)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("mcp-guard not found in PATH: %w", err)
	}

	t.pending = make(map[int64]chan *JSONRPCResponse)

	cmd := t.newCmd(path, t.args...)
	cmd.Env = []string{}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		t.mu.Unlock()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	t.cmd = cmd
	t.stdin = stdin
	t.stdout = stdout

	if err := cmd.Start(); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("start mcp-guard: %w", err)
	}

	t.wg.Add(1)
	go t.readLoop(stdout)

	select {
	case <-ctx.Done():
		t.mu.Unlock()
		_ = t.Close()
		return fmt.Errorf("connect cancelled: %w", ctx.Err())
	default:
		t.mu.Unlock()
		return nil
	}
}

func (t *StdioTransport) readLoop(r io.Reader) {
	defer t.wg.Done()
	dec := json.NewDecoder(r)

	for {
		var resp JSONRPCResponse
		if err := dec.Decode(&resp); err != nil {
			if err == io.EOF {
				return
			}
			t.mu.Lock()
			t.readErr = fmt.Errorf("decode JSON-RPC response: %w", err)
			for id, ch := range t.pending {
				select {
				case ch <- &JSONRPCResponse{
					ID: id,
					Error: &JSONRPCError{
						Code:    -32700,
						Message: "parse error: " + err.Error(),
					},
				}:
				default:
				}
				delete(t.pending, id)
			}
			t.mu.Unlock()
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
			default:
			}
		}
	}
}

// Send sends a JSON-RPC request and waits for the response.
func (t *StdioTransport) Send(ctx context.Context, method string, params interface{}) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	stdin := t.stdin
	if stdin == nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport not connected")
	}
	id := atomic.AddInt64(&t.nextID, 1)
	ch := make(chan *JSONRPCResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	t.writeMu.Lock()
	_, err = fmt.Fprintln(stdin, string(data))
	t.writeMu.Unlock()

	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("request %s cancelled: %w", method, ctx.Err())
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *StdioTransport) Notify(ctx context.Context, method string, params interface{}) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	t.mu.Lock()
	stdin := t.stdin
	closed := t.closed
	t.mu.Unlock()

	if closed {
		return fmt.Errorf("transport closed")
	}
	if stdin == nil {
		return fmt.Errorf("transport not connected")
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("notification %s cancelled: %w", method, ctx.Err())
	default:
	}

	if _, err := fmt.Fprintln(stdin, string(data)); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	return nil
}

// Close gracefully shuts down the transport.
func (t *StdioTransport) Close() error {
	t.mu.Lock()

	if t.closed && t.cmd == nil {
		t.mu.Unlock()
		return nil
	}
	t.closed = true

	for id, ch := range t.pending {
		select {
		case ch <- &JSONRPCResponse{
			ID: id,
			Error: &JSONRPCError{
				Code:    -32000,
				Message: "transport closed",
			},
		}:
		default:
		}
		delete(t.pending, id)
	}

	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			// ignore close errors on cleanup
		}
		t.stdin = nil
	}

	cmd := t.cmd
	t.cmd = nil
	t.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
			// ignore process exit status on close
		case <-time.After(5 * time.Second):
			if err := cmd.Process.Kill(); err != nil {
				// ignore kill errors on cleanup
			}
			_ = <-done // drain
		}
	}

	t.wg.Wait()

	return nil
}
