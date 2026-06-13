package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeAppRunner is a test double for the application layer.
type fakeAppRunner struct {
	mu sync.Mutex

	setYoloCalled bool

	startSessionReturn *api.Session
	startSessionErr    error

	resumeSessionID     string
	resumeSessionReturn *api.Session
	resumeSessionErr    error

	runTurnSessionID string
	runTurnInput     string
	runTurnReturn    <-chan api.TurnEvent
	runTurnErr       error

	closeErr error
}

func (f *fakeAppRunner) SetYolo(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setYoloCalled = v
}

func (f *fakeAppRunner) StartSession(_ context.Context) (*api.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startSessionReturn, f.startSessionErr
}

func (f *fakeAppRunner) ResumeSession(_ context.Context, id string) (*api.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeSessionID = id
	return f.resumeSessionReturn, f.resumeSessionErr
}

func (f *fakeAppRunner) RunTurn(ctx context.Context, sessionID, input string) (<-chan api.TurnEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runTurnSessionID = sessionID
	f.runTurnInput = input
	if f.runTurnReturn == nil {
		ch := make(chan api.TurnEvent)
		close(ch)
		return ch, f.runTurnErr
	}

	// Wrap the configured channel so cancellation closes the output channel.
	out := make(chan api.TurnEvent)
	go func() {
		defer close(out)
		for {
			select {
			case ev, ok := <-f.runTurnReturn:
				if !ok {
					return
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, f.runTurnErr
}

func (f *fakeAppRunner) Close() error {
	return f.closeErr
}

// parseLine extracts a JSON-RPC response from a JSON line.
func parseLine(t *testing.T, line string) jsonRPCResponse {
	t.Helper()
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// send writes a JSON-RPC request line to stdin.
func send(t *testing.T, w *io.PipeWriter, method string, id any, params any) {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		req["id"] = id
	}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func TestServer_Initialize(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d: %s", len(lines), stdout.String())
	}
	resp := parseLine(t, lines[0])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.ID != float64(1) {
		t.Fatalf("expected id 1, got %v", resp.ID)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", resp.Result)
	}
	if result["protocolVersion"] != float64(1) {
		t.Fatalf("unexpected protocol version: %v", result["protocolVersion"])
	}
}

func TestServer_SessionNewAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	app := &fakeAppRunner{
		startSessionReturn:  &api.Session{ID: "sess-new", Path: tmpDir},
		resumeSessionReturn: &api.Session{ID: "sess-123", Path: tmpDir},
	}
	srv := NewServer(app, slog.Default())

	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"workingDir":"` + tmpDir + `"}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/load","params":{"sessionId":"sess-123"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), stdout.String())
	}

	resp1 := parseLine(t, lines[0])
	if resp1.Error != nil {
		t.Fatalf("session/new error: %v", resp1.Error)
	}
	res1, _ := resp1.Result.(map[string]any)
	if res1["sessionId"] != "sess-new" {
		t.Fatalf("expected sessionId sess-new, got %v", res1["sessionId"])
	}

	resp2 := parseLine(t, lines[1])
	if resp2.Error != nil {
		t.Fatalf("session/load error: %v", resp2.Error)
	}
	res2, _ := resp2.Result.(map[string]any)
	if res2["sessionId"] != "sess-123" {
		t.Fatalf("expected sessionId sess-123, got %v", res2["sessionId"])
	}
	if app.resumeSessionID != "sess-123" {
		t.Fatalf("expected resume session id sess-123, got %s", app.resumeSessionID)
	}
}

func TestServer_PromptReturnsFinalResponse(t *testing.T) {
	ch := make(chan api.TurnEvent, 3)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "Hello, "}
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "world!"}
	ch <- api.TurnEvent{Type: api.TurnEventDone, Content: "Hello, world!"}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-prompt", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())

	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"say hello"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %s", len(lines), stdout.String())
	}

	resp := parseLine(t, lines[3])
	if resp.Error != nil {
		t.Fatalf("prompt error: %v", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["response"] != "Hello, world!" {
		t.Fatalf("unexpected response: %v", result["response"])
	}
	if !app.setYoloCalled {
		t.Error("expected SetYolo(true) to be called")
	}
	if app.runTurnInput != "say hello" {
		t.Fatalf("expected prompt %q, got %q", "say hello", app.runTurnInput)
	}
}

func TestServer_PromptStreamsUpdates(t *testing.T) {
	ch := make(chan api.TurnEvent, 4)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "thinking"}
	ch <- api.TurnEvent{Type: api.TurnEventToolResult, Result: api.ToolResult{CallID: "call-1", Name: "read_file", Output: "data"}}
	ch <- api.TurnEvent{Type: api.TurnEventApprovalRequest, ToolCalls: []api.ToolCall{{ID: "call-2", Name: "write_file"}}}
	ch <- api.TurnEvent{Type: api.TurnEventDone, Content: "done"}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-stream", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())

	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"stream"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d: %s", len(lines), stdout.String())
	}

	// First update: agent_message_chunk
	var update1 jsonRPCNotification
	if err := json.Unmarshal([]byte(lines[1]), &update1); err != nil {
		t.Fatalf("unmarshal update1: %v", err)
	}
	if update1.Method != "session/update" {
		t.Fatalf("expected session/update, got %s", update1.Method)
	}
	params1, _ := update1.Params.(map[string]any)
	if params1["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("unexpected update type: %v", params1["sessionUpdate"])
	}

	// Second update: tool_result
	var update2 jsonRPCNotification
	if err := json.Unmarshal([]byte(lines[2]), &update2); err != nil {
		t.Fatalf("unmarshal update2: %v", err)
	}
	params2, _ := update2.Params.(map[string]any)
	if params2["sessionUpdate"] != "tool_result" {
		t.Fatalf("unexpected update type: %v", params2["sessionUpdate"])
	}

	// Third update: approval_request
	var update3 jsonRPCNotification
	if err := json.Unmarshal([]byte(lines[3]), &update3); err != nil {
		t.Fatalf("unmarshal update3: %v", err)
	}
	params3, _ := update3.Params.(map[string]any)
	if params3["sessionUpdate"] != "approval_request" {
		t.Fatalf("unexpected update type: %v", params3["sessionUpdate"])
	}
}

func TestServer_PromptErrorReturnsError(t *testing.T) {
	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventError, Error: errors.New("model unreachable")}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-err", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())

	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"fail"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), stdout.String())
	}
	resp := parseLine(t, lines[1])
	if resp.Error == nil {
		t.Fatal("expected prompt error response")
	}
	if resp.Error.Code != -32603 {
		t.Fatalf("expected error code -32603, got %d", resp.Error.Code)
	}
}

func TestServer_CancelAbortsLongPrompt(t *testing.T) {
	ch := make(chan api.TurnEvent)
	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-cancel", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())

	rStdin, wStdin := io.Pipe()
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = srv.Run(ctx, rStdin, &stdout)
	}()

	send(t, wStdin, "session/new", 1, map[string]any{})
	time.Sleep(50 * time.Millisecond)
	send(t, wStdin, "session/prompt", 2, map[string]any{"prompt": "slow"})
	time.Sleep(50 * time.Millisecond)
	send(t, wStdin, "session/cancel", nil, map[string]any{})
	time.Sleep(50 * time.Millisecond)
	wStdin.Close()

	wg.Wait()

	if runErr != nil && !errors.Is(runErr, context.Canceled) && !strings.Contains(runErr.Error(), "context canceled") {
		t.Fatalf("unexpected run error: %v", runErr)
	}
	if app.runTurnInput != "slow" {
		t.Fatalf("expected prompt slow, got %q", app.runTurnInput)
	}
}

func TestServer_InvalidJSONRPC(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader("not json\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	resp := parseLine(t, lines[0])
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Fatalf("expected parse error code -32700, got %d", resp.Error.Code)
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"unknown"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected method not found, got %v", resp.Error)
	}
}

func TestServer_PromptWithoutSession(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"prompt":"hi"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected no active session error, got %v", resp.Error)
	}
}
