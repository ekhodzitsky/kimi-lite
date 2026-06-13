// Package mcp provides an MCP client implementation using JSON-RPC over stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// isClosedError reports whether err signals that the transport's underlying
// reader or process has closed. It treats io.EOF and os.ErrClosed as clean
// closes, and also catches the "file already closed" error returned by some
// platforms when a pipe is closed concurrently.
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return true
	}
	return strings.Contains(err.Error(), "file already closed")
}

const (
	maxFrameSize = 8 * 1024 * 1024 // 8 MB
	stderrCap    = 16 * 1024       // 16 KB
)

// Transport defines the interface for MCP JSON-RPC transports.
type Transport interface {
	// Connect establishes the underlying connection.
	Connect(ctx context.Context) error
	// Send sends a JSON-RPC request and waits for a matching response.
	Send(ctx context.Context, method string, params any) (*JSONRPCResponse, error)
	// Notify sends a JSON-RPC notification (no response expected).
	Notify(ctx context.Context, method string, params any) error
	// Close gracefully shuts down the transport.
	Close() error
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCNotification represents a JSON-RPC 2.0 notification (no ID field).
type JSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
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
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *JSONRPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// boundedBuffer is a fixed-size ring buffer for capturing stderr.
type boundedBuffer struct {
	buf []byte
	n   int
}

func newBoundedBuffer(cap int) *boundedBuffer {
	return &boundedBuffer{buf: make([]byte, 0, cap)}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	for _, c := range p {
		if len(b.buf) < cap(b.buf) {
			b.buf = append(b.buf, c)
		} else {
			b.buf[b.n] = c
			b.n = (b.n + 1) % cap(b.buf)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	if len(b.buf) < cap(b.buf) {
		return string(b.buf)
	}
	ordered := make([]byte, cap(b.buf))
	copy(ordered, b.buf[b.n:])
	copy(ordered[len(b.buf)-b.n:], b.buf[:b.n])
	return string(ordered)
}

// minimalEnv returns a minimal environment for MCP child processes.
// It preserves PATH, HOME, LANG, and TMPDIR while excluding variables
// that likely contain secrets.
func minimalEnv() []string {
	allow := map[string]struct{}{
		"PATH":   {},
		"HOME":   {},
		"LANG":   {},
		"TMPDIR": {},
		"USER":   {},
		"SHELL":  {},
	}
	secretPatterns := []string{
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
		"API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY",
		"AUTH", "BEARER", "JWT",
	}

	var out []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		upper := strings.ToUpper(key)
		if _, ok := allow[upper]; ok {
			out = append(out, e)
			continue
		}
		if strings.Contains(upper, "SSH") {
			continue
		}
		safe := true
		for _, p := range secretPatterns {
			if strings.Contains(upper, p) {
				safe = false
				break
			}
		}
		if safe {
			out = append(out, e)
		}
	}
	return out
}

// StdioTransport implements Transport using stdin/stdout of a subprocess.
type StdioTransport struct {
	command string
	args    []string

	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	pending      map[int64]chan *JSONRPCResponse
	nextID       int64
	closed       bool
	wg           sync.WaitGroup
	readErr      error
	newCmd       func(name string, arg ...string) *exec.Cmd
	writeMu      sync.Mutex
	stderr       *boundedBuffer
	cmdWaitCh    chan struct{}
	cmdErr       error
	cmdWaited    sync.Once
	logger       *slog.Logger
	closeTimeout time.Duration
}

// NewStdioTransport creates a new stdio transport for the given command.
func NewStdioTransport(command string, args ...string) *StdioTransport {
	return &StdioTransport{
		command:      command,
		args:         args,
		newCmd:       exec.Command,
		logger:       slog.Default(),
		closeTimeout: 5 * time.Second,
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
	// Provide a curated minimal environment so the child can resolve
	// binaries and access $HOME, but does not inherit secrets.
	cmd.Env = minimalEnv()
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

	// Capture stderr in a bounded buffer so diagnostics are available
	// if the handshake or start fails.
	stderrBuf := newBoundedBuffer(stderrCap)
	cmd.Stderr = stderrBuf

	t.cmd = cmd
	t.stdin = stdin
	t.stdout = stdout
	t.stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		t.cmd = nil
		t.stdin = nil
		t.stdout = nil
		t.mu.Unlock()
		return fmt.Errorf("start mcp-guard: %w (stderr: %s)", err, stderrBuf.String())
	}

	// Start a single goroutine that waits for the process.
	// cmd.Wait() may only be called once.
	t.cmdWaitCh = make(chan struct{})
	t.cmdWaited.Do(func() {
		go func() {
			t.cmdErr = cmd.Wait()
			close(t.cmdWaitCh)
		}()
	})

	// Non-blocking check: if the process exits immediately, capture stderr.
	select {
	case <-t.cmdWaitCh:
		if t.cmdErr != nil {
			t.mu.Unlock()
			_ = t.Close()
			return fmt.Errorf("mcp-guard exited immediately: %w (stderr: %s)", t.cmdErr, stderrBuf.String())
		}
	case <-time.After(50 * time.Millisecond):
		// Process is still running, proceed.
	}

	t.wg.Add(1)
	go t.readLoop(stdout)

	// The connect ctx bounds only process spawn + the handshake
	// (via Send in client.Connect), not the session lifetime — the
	// readLoop goroutine and subprocess intentionally outlive the
	// connect ctx until Close.
	t.mu.Unlock()
	return nil
}

func (t *StdioTransport) readLoop(r io.Reader) {
	defer t.wg.Done()
	reader := bufio.NewReader(r)

	for {
		// Read line-delimited frames with a size cap.
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if isClosedError(err) {
				t.mu.Lock()
				t.readErr = fmt.Errorf("mcp transport: subprocess closed connection")
				for id, ch := range t.pending {
					ch <- &JSONRPCResponse{
						ID: id,
						Error: &JSONRPCError{
							Code:    -32000,
							Message: "transport closed",
						},
					}
					delete(t.pending, id)
				}
				t.mu.Unlock()
				return
			}
			t.mu.Lock()
			t.readErr = fmt.Errorf("read JSON-RPC frame: %w", err)
			for id, ch := range t.pending {
				ch <- &JSONRPCResponse{
					ID: id,
					Error: &JSONRPCError{
						Code:    -32700,
						Message: "parse error: " + err.Error(),
					},
				}
				delete(t.pending, id)
			}
			t.mu.Unlock()
			return
		}

		if len(line) > maxFrameSize {
			t.mu.Lock()
			t.readErr = fmt.Errorf("frame exceeds max size (%d bytes)", maxFrameSize)
			for id, ch := range t.pending {
				ch <- &JSONRPCResponse{
					ID: id,
					Error: &JSONRPCError{
						Code:    -32700,
						Message: "frame too large",
					},
				}
				delete(t.pending, id)
			}
			t.mu.Unlock()
			return
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			t.mu.Lock()
			t.readErr = fmt.Errorf("decode JSON-RPC response: %w", err)
			for id, ch := range t.pending {
				ch <- &JSONRPCResponse{
					ID: id,
					Error: &JSONRPCError{
						Code:    -32700,
						Message: "parse error: " + err.Error(),
					},
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
			ch <- &resp
		} else if t.logger != nil {
			t.logger.Debug("dropping unmatched JSON-RPC frame", "id", resp.ID)
		}
	}
}

// Send sends a JSON-RPC request and waits for the response.
func (t *StdioTransport) Send(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	if t.readErr != nil {
		t.mu.Unlock()
		return nil, t.readErr
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
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := fmt.Fprintln(stdin, string(data))
		writeErrCh <- err
	}()
	select {
	case <-ctx.Done():
		t.writeMu.Unlock()
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("request %s cancelled: %w", method, ctx.Err())
	case err := <-writeErrCh:
		t.writeMu.Unlock()
		if err != nil {
			t.mu.Lock()
			readErr := t.readErr
			delete(t.pending, id)
			t.mu.Unlock()
			if readErr != nil {
				return nil, readErr
			}
			return nil, fmt.Errorf("write request: %w", err)
		}
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
func (t *StdioTransport) Notify(ctx context.Context, method string, params any) error {
	req := JSONRPCNotification{
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

	writeErrCh := make(chan error, 1)
	go func() {
		_, err := fmt.Fprintln(stdin, string(data))
		writeErrCh <- err
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("notification %s cancelled: %w", method, ctx.Err())
	case err := <-writeErrCh:
		if err != nil {
			t.mu.Lock()
			readErr := t.readErr
			t.mu.Unlock()
			if readErr != nil {
				return readErr
			}
			return fmt.Errorf("write notification: %w", err)
		}
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
	t.readErr = fmt.Errorf("transport closed")

	for id, ch := range t.pending {
		ch <- &JSONRPCResponse{
			ID: id,
			Error: &JSONRPCError{
				Code:    -32000,
				Message: "transport closed",
			},
		}
		delete(t.pending, id)
	}

	stdin := t.stdin
	t.stdin = nil
	cmd := t.cmd
	t.cmd = nil
	t.mu.Unlock()

	t.writeMu.Lock()
	if stdin != nil {
		_ = stdin.Close() // ignore close errors on cleanup
	}
	t.writeMu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Ensure the wait goroutine is started (it may have been started in Connect).
		t.cmdWaited.Do(func() {
			go func() {
				t.cmdErr = cmd.Wait()
				close(t.cmdWaitCh)
			}()
		})

		grace := t.closeTimeout
		if grace <= 0 {
			grace = 5 * time.Second
		}
		timer := time.AfterFunc(grace, func() {
			_ = cmd.Process.Kill() // ignore kill errors on cleanup
		})
		<-t.cmdWaitCh
		timer.Stop()
	}

	t.wg.Wait()

	return nil
}
