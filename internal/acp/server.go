package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// notificationKey marks a request context as a JSON-RPC notification (no id).
type notificationKey struct{}

// withNotification returns a context that signals the request is a notification.
func withNotification(ctx context.Context) context.Context {
	return context.WithValue(ctx, notificationKey{}, true)
}

// isNotification reports whether ctx was marked as a JSON-RPC notification.
func isNotification(ctx context.Context) bool {
	v, _ := ctx.Value(notificationKey{}).(bool)
	return v
}

// maxFrameSize limits the size of a single JSON-RPC frame read from stdin.
// It protects the server from unbounded memory growth and replaces the
// 64 KB default limit of bufio.Scanner.
const maxFrameSize = 8 * 1024 * 1024 // 8 MB

// appRunner is the application interface consumed by the ACP server.
// It mirrors the interface used by cmd/kimi-lite/main.go so that *app.App
// satisfies it without an adapter.
type appRunner interface {
	SetYolo(bool)
	ResumeSession(ctx context.Context, id string) (*api.Session, error)
	StartSession(ctx context.Context) (*api.Session, error)
	RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error)
	Close() error
}

// Server speaks JSON-RPC 2.0 over stdio and exposes ACP methods.
type Server struct {
	app         appRunner
	logger      *slog.Logger
	allowedRoot string

	mu             sync.RWMutex
	session        *api.Session
	promptInFlight bool
	cancelFn       context.CancelFunc

	writeMu sync.Mutex
}

// NewServer creates an ACP server backed by the provided application.
func NewServer(application appRunner, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		app:    application,
		logger: logger,
	}
}

// SetAllowedRoot sets the root directory that session working directories must
// remain within. An empty root disables the check.
func (s *Server) SetAllowedRoot(root string) {
	s.allowedRoot = root
}

// Run reads JSON-RPC requests from stdin and writes responses to stdout until
// stdin is closed or the context is cancelled.
func (s *Server) Run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	defer func() {
		if err := s.app.Close(); err != nil {
			s.logger.Error("app close failed", "error", err)
		}
	}()

	reader := bufio.NewReader(stdin)
	enc := json.NewEncoder(stdout)
	var wg sync.WaitGroup

	for {
		type readResult struct {
			line []byte
			err  error
		}
		readCh := make(chan readResult, 1)
		go func() {
			l, e := readLine(reader, maxFrameSize)
			readCh <- readResult{line: l, err: e}
		}()

		var line []byte
		select {
		case <-ctx.Done():
			wg.Wait()
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		case res := <-readCh:
			line = res.line
			if res.err != nil {
				wg.Wait()
				if errors.Is(res.err, io.EOF) {
					return nil
				}
				return fmt.Errorf("read stdin: %w", res.err)
			}
		}

		// Remove trailing newline/CR for cleaner logging and parsing.
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})

		if len(line) > maxFrameSize {
			if werr := s.writeError(ctx, enc, nil, -32700, "parse error", fmt.Errorf("frame exceeds maximum size of %d bytes", maxFrameSize)); werr != nil {
				wg.Wait()
				return fmt.Errorf("write oversized frame error: %w", werr)
			}
			continue
		}

		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if werr := s.writeError(ctx, enc, nil, -32700, "parse error", err); werr != nil {
				wg.Wait()
				return fmt.Errorf("write parse error: %w", werr)
			}
			continue
		}

		// Prompts may block; run them concurrently so session/cancel can be
		// read from stdin while a prompt is in flight.
		if req.Method == "session/prompt" {
			wg.Add(1)
			go func(r jsonRPCRequest) {
				defer wg.Done()
				if err := s.handle(ctx, r, enc); err != nil {
					s.logger.Error("prompt handler failed", "error", err)
				}
			}(req)
			continue
		}

		if err := s.handle(ctx, req, enc); err != nil {
			// Notifications have no response channel; log the error and keep reading
			// rather than tearing down the server.
			if req.ID == nil {
				s.logger.Error("notification handler failed", "method", req.Method, "error", err)
				continue
			}
			wg.Wait()
			return err
		}
	}
}

// handle dispatches a single JSON-RPC request.
func (s *Server) handle(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	if req.JSONRPC != "2.0" {
		return s.writeError(ctx, enc, req.ID, -32600, "invalid request", errors.New("invalid jsonrpc version"))
	}

	// Notifications omit the id field; suppress responses for them.
	if req.ID == nil {
		ctx = withNotification(ctx)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req, enc)
	case "session/new":
		return s.handleSessionNew(ctx, req, enc)
	case "session/load":
		return s.handleSessionLoad(ctx, req, enc)
	case "session/prompt":
		return s.handleSessionPrompt(ctx, req, enc)
	case "session/cancel":
		return s.handleSessionCancel(ctx, req, enc)
	default:
		return s.writeError(ctx, enc, req.ID, -32601, "method not found", fmt.Errorf("%q", req.Method))
	}
}

// writeError writes a JSON-RPC error response. If the request was a
// notification (id omitted), no response is written.
func (s *Server) writeError(ctx context.Context, enc *json.Encoder, id any, code int, message string, cause error) error {
	if id == nil && isNotification(ctx) {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	errObj := &jsonRPCError{
		Code:    code,
		Message: message,
	}
	if cause != nil {
		// Sanitize: log the internal cause server-side but do not forward raw
		// error strings to the JSON-RPC client.
		s.logger.Error("jsonrpc handler error", "code", code, "message", message, "cause", cause)
		errObj.Data = "internal error"
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   errObj,
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encode error response: %w", err)
	}
	return nil
}

// writeResult writes a JSON-RPC success response. If the request was a
// notification (id omitted), no response is written.
func (s *Server) writeResult(ctx context.Context, enc *json.Encoder, id any, result any) error {
	if id == nil && isNotification(ctx) {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encode result response: %w", err)
	}
	return nil
}

// writeNotification writes a JSON-RPC notification.
func (s *Server) writeNotification(ctx context.Context, enc *json.Encoder, method string, params any) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	n := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := enc.Encode(n); err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	return nil
}

// currentSession returns the active session or an error.
func (s *Server) currentSession() (*api.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil {
		return nil, errors.New("no active session")
	}
	return s.session, nil
}

// setSession updates the active session.
func (s *Server) setSession(sess *api.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = sess
}

// startPrompt attempts to mark a prompt as in-flight. It returns false if
// another prompt is already running.
func (s *Server) startPrompt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptInFlight {
		return false
	}
	s.promptInFlight = true
	return true
}

// endPrompt clears the in-flight prompt flag and its cancel function.
func (s *Server) endPrompt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptInFlight = false
	s.cancelFn = nil
}

// setCancel stores the cancel function for the current prompt.
func (s *Server) setCancel(fn context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFn = fn
}

// cancelCurrent cancels the in-flight prompt if any. It returns true if a
// prompt was actually cancelled.
func (s *Server) cancelCurrent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.promptInFlight || s.cancelFn == nil {
		return false
	}
	s.cancelFn()
	s.cancelFn = nil
	return true
}

// readLine reads a single line terminated by '\n' from r, capping the returned
// slice at limit+1 bytes. This bounds per-frame memory even when an input line
// far exceeds maxFrameSize; the remainder of an oversized line is discarded as
// it is read.
func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return line, fmt.Errorf("read byte: %w", err)
		}
		if len(line) <= limit {
			line = append(line, b)
		}
		if b == '\n' {
			break
		}
	}
	return line, nil
}

// changeWorkingDir validates dir against allowedRoot and switches to it when
// non-empty and different from the current working directory.
func changeWorkingDir(dir, allowedRoot string) error {
	if dir == "" {
		return nil
	}
	if allowedRoot != "" {
		rel, err := filepath.Rel(allowedRoot, dir)
		if err != nil || strings.Contains(rel, "..") || rel == ".." {
			return fmt.Errorf("working directory %q is outside allowed root %q", dir, allowedRoot)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat working directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	// If the current directory has been deleted, os.Getwd() can fail. In that
	// case we still attempt to chdir to dir, since dir was already validated.
	cwd, err := os.Getwd()
	if err == nil && cwd == dir {
		return nil
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("change working directory: %w", err)
	}
	return nil
}
