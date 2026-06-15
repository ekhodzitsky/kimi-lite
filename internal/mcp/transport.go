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
	"sort"
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

// frameReader reads newline-delimited frames from an io.Reader while
// enforcing a maximum frame size. It uses a bufio.Reader so data is buffered
// without losing bytes that follow a frame delimiter, and oversized frames are
// rejected before they can cause excessive memory allocation.
type frameReader struct {
	r       *bufio.Reader
	maxSize int
}

func newFrameReader(r io.Reader, maxSize int) *frameReader {
	return &frameReader{
		r:       bufio.NewReader(r),
		maxSize: maxSize,
	}
}

func (fr *frameReader) readFrame() ([]byte, error) {
	var frame []byte
	for {
		b, err := fr.r.ReadByte()
		if err != nil {
			return frame, fmt.Errorf("read frame byte: %w", err)
		}
		frame = append(frame, b)
		if b == '\n' {
			return frame, nil
		}
		if len(frame) > fr.maxSize {
			return nil, fmt.Errorf("frame exceeds max size (%d bytes)", fr.maxSize)
		}
	}
}

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

// boundedBuffer is a fixed-size ring buffer for capturing stderr. It is safe
// for concurrent writes and String calls.
type boundedBuffer struct {
	mu  sync.Mutex
	buf []byte
	n   int
}

func newBoundedBuffer(cap int) *boundedBuffer {
	return &boundedBuffer{buf: make([]byte, 0, cap)}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < cap(b.buf) {
		return string(b.buf)
	}
	ordered := make([]byte, cap(b.buf))
	copy(ordered, b.buf[b.n:])
	copy(ordered[len(b.buf)-b.n:], b.buf[:b.n])
	return string(ordered)
}

// buildEnv returns the environment for the subprocess, starting from the
// minimal environment and overlaying any configured Env values.
func (t *StdioTransport) buildEnv() []string {
	base := minimalEnv()
	if len(t.env) == 0 {
		return base
	}
	m := make(map[string]string, len(base))
	for _, e := range base {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	for k, v := range t.env {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(m))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
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
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASSPHRASE", "CREDENTIAL",
		"API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY", "SECRET_KEY",
		"AUTH", "BEARER", "JWT", "OAUTH", "SSO",
		"CERT", "PRIVATE", "SIGNATURE", "COOKIE", "SESSION", "HOOK",
		"CREDIT", "PAN", "CVV", "PIN",
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
	env     map[string]string
	cwd     string

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
	writeWg      sync.WaitGroup
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

// SetEnv sets extra environment variables for the subprocess.
func (t *StdioTransport) SetEnv(env map[string]string) {
	copied := make(map[string]string, len(env))
	for k, v := range env {
		copied[k] = v
	}
	t.env = copied
}

// SetCWD sets the working directory for the subprocess.
func (t *StdioTransport) SetCWD(cwd string) {
	t.cwd = cwd
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
		return fmt.Errorf("mcp command %q not found in PATH: %w", t.command, err)
	}

	t.pending = make(map[int64]chan *JSONRPCResponse)

	cmd := t.newCmd(path, t.args...)
	// Provide a curated minimal environment so the child can resolve
	// binaries and access $HOME, but does not inherit secrets.
	cmd.Env = t.buildEnv()
	if t.cwd != "" {
		cmd.Dir = t.cwd
	}
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
	t.cmdWaited = sync.Once{}
	t.cmdWaited.Do(func() {
		go func() {
			t.cmdErr = cmd.Wait()
			close(t.cmdWaitCh)
		}()
	})

	// Non-blocking check: if the process has already exited, capture stderr.
	select {
	case <-t.cmdWaitCh:
		if t.cmdErr != nil {
			t.mu.Unlock()
			_ = t.Close()
			return fmt.Errorf("mcp-guard exited immediately: %w (stderr: %s)", t.cmdErr, stderrBuf.String())
		}
	default:
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
	fr := newFrameReader(r, maxFrameSize)

	for {
		// Read line-delimited frames with a size cap.
		line, err := fr.readFrame()
		if err != nil {
			if len(line) > 0 {
				t.mu.Lock()
				t.readErr = fmt.Errorf("incomplete JSON-RPC frame")
				for id, ch := range t.pending {
					ch <- &JSONRPCResponse{
						ID: id,
						Error: &JSONRPCError{
							Code:    -32700,
							Message: "parse error: incomplete frame",
						},
					}
					delete(t.pending, id)
				}
				t.mu.Unlock()
				return
			}
			if isClosedError(err) {
				t.mu.Lock()
				stderr := ""
				if t.stderr != nil {
					stderr = t.stderr.String()
				}
				if stderr != "" {
					t.readErr = fmt.Errorf("mcp transport: subprocess closed connection (stderr: %s)", stderr)
				} else {
					t.readErr = fmt.Errorf("mcp transport: subprocess closed connection")
				}
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
			// Copy the response before sending so the pointer does not
			// reference the loop variable, which would race with the next
			// iteration.
			respCopy := resp
			ch <- &respCopy
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
	t.writeWg.Add(1)
	go func() {
		defer t.writeWg.Done()
		_, err := fmt.Fprintln(stdin, string(data))
		writeErrCh <- err
	}()
	select {
	case <-ctx.Done():
		t.writeMu.Unlock()
		// The writer goroutine may still be running; Close will wait for it
		// after closing stdin so we do not race on the pipe lifecycle.
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
	t.writeWg.Add(1)
	go func() {
		defer t.writeWg.Done()
		_, err := fmt.Fprintln(stdin, string(data))
		writeErrCh <- err
	}()
	select {
	case <-ctx.Done():
		// The writer goroutine may still be running; Close will wait for it
		// after closing stdin so we do not race on the pipe lifecycle.
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
	waitCh := t.cmdWaitCh
	t.mu.Unlock()

	// Start the kill timer before acquiring writeMu so a blocked writer does
	// not prevent us from terminating the subprocess.
	var timer *time.Timer
	if cmd != nil && cmd.Process != nil && waitCh != nil {
		// Ensure the wait goroutine is started (it may have been started in Connect).
		t.cmdWaited.Do(func() {
			go func() {
				t.cmdErr = cmd.Wait()
				close(waitCh)
			}()
		})

		grace := t.closeTimeout
		if grace <= 0 {
			grace = 5 * time.Second
		}
		timer = time.AfterFunc(grace, func() {
			_ = cmd.Process.Kill() // ignore kill errors on cleanup
		})
	}

	t.writeMu.Lock()
	if stdin != nil {
		_ = stdin.Close() // ignore close errors on cleanup
	}
	t.writeMu.Unlock()

	// Wait for any in-flight stdin writers to finish before reaping the
	// process so they do not write to a closed pipe.
	t.writeWg.Wait()

	if waitCh != nil {
		<-waitCh
		if timer != nil {
			timer.Stop()
		}
	}

	t.wg.Wait()

	return nil
}
