package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

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
	app    appRunner
	logger *slog.Logger

	mu       sync.Mutex
	session  *api.Session
	cancelFn context.CancelFunc

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

// Run reads JSON-RPC requests from stdin and writes responses to stdout until
// stdin is closed or the context is cancelled.
func (s *Server) Run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(stdin)
	enc := json.NewEncoder(stdout)
	var wg sync.WaitGroup

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			wg.Wait()
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if werr := s.writeError(ctx, enc, nil, -32700, "parse error", err); werr != nil {
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
			wg.Wait()
			return err
		}
	}

	wg.Wait()

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("scan stdin: %w", err)
	}
	return nil
}

// handle dispatches a single JSON-RPC request.
func (s *Server) handle(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	if req.JSONRPC != "2.0" {
		return s.writeError(ctx, enc, req.ID, -32600, "invalid request", errors.New("invalid jsonrpc version"))
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

// writeError writes a JSON-RPC error response.
func (s *Server) writeError(ctx context.Context, enc *json.Encoder, id any, code int, message string, cause error) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    code,
			Message: message,
			Data:    cause.Error(),
		},
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encode error response: %w", err)
	}
	return nil
}

// writeResult writes a JSON-RPC success response.
func (s *Server) writeResult(ctx context.Context, enc *json.Encoder, id any, result any) error {
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
	s.mu.Lock()
	defer s.mu.Unlock()
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

// setCancel stores the cancel function for the current prompt.
func (s *Server) setCancel(fn context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFn = fn
}

// cancelCurrent cancels the in-flight prompt if any.
func (s *Server) cancelCurrent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
}

// changeWorkingDir switches to dir when non-empty and different from cwd.
func changeWorkingDir(dir string) error {
	if dir == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if cwd == dir {
		return nil
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("change working directory: %w", err)
	}
	return nil
}
