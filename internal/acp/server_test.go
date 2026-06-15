package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

	// runTurnBlock, when non-nil, blocks RunTurn until it is closed.
	runTurnBlock <-chan struct{}
	// runTurnStarted is closed by RunTurn once it has begun; useful for
	// synchronizing overlapping prompt tests.
	runTurnStarted chan struct{}

	closeCalled bool
	closeErr    error
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

	if f.runTurnStarted != nil {
		close(f.runTurnStarted)
		f.runTurnStarted = nil
	}

	if f.runTurnBlock != nil {
		select {
		case <-f.runTurnBlock:
		case <-ctx.Done():
		}
	}

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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalled = true
	return f.closeErr
}

func (f *fakeAppRunner) wasClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalled
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
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

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
	if app.setYoloCalled {
		t.Error("expected SetYolo NOT to be called by ACP prompt")
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
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %s", len(lines), stdout.String())
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

	// Approval requests are not supported over ACP, so the prompt returns an error.
	resp := parseLine(t, lines[3])
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected prompt error for approval request, got %v", resp.Error)
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

func TestServer_AppCloseCalled(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !app.wasClosed() {
		t.Fatal("expected app.Close to be called")
	}
}

func TestServer_OverlappingPromptsRejected(t *testing.T) {
	ch := make(chan api.TurnEvent)
	block := make(chan struct{})
	started := make(chan struct{})
	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-overlap", Path: "/tmp"},
		runTurnReturn:      ch,
		runTurnBlock:       block,
		runTurnStarted:     started,
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
	send(t, wStdin, "session/prompt", 2, map[string]any{"prompt": "first"})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not start")
	}

	send(t, wStdin, "session/prompt", 3, map[string]any{"prompt": "second"})
	time.Sleep(50 * time.Millisecond)
	close(block)
	close(ch)
	time.Sleep(50 * time.Millisecond)
	wStdin.Close()

	wg.Wait()

	if runErr != nil && !errors.Is(runErr, context.Canceled) && !strings.Contains(runErr.Error(), "context canceled") {
		t.Fatalf("unexpected run error: %v", runErr)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d: %s", len(lines), stdout.String())
	}

	// Find the response for the second prompt (id 3); it must be an error.
	var found bool
	for _, line := range lines {
		resp := parseLine(t, line)
		if resp.ID == float64(3) {
			found = true
			if resp.Error == nil || resp.Error.Code != -32603 {
				t.Fatalf("expected overlapping prompt error, got %v", resp.Error)
			}
			break
		}
	}
	if !found {
		t.Fatalf("did not find response for second prompt in %s", stdout.String())
	}
}

func TestServer_LargeFrameAccepted(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())

	// Build a frame larger than the 64 KB bufio.Scanner limit but smaller
	// than the 8 MB max frame size to exercise the new reader path.
	large := make([]byte, 128*1024)
	for i := range large {
		large[i] = 'x'
	}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"padding":"` + string(large) + `"}}` + "\n"

	stdin := strings.NewReader(req)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error != nil {
		t.Fatalf("expected success for large frame, got %v", resp.Error)
	}
	if resp.ID != float64(1) {
		t.Fatalf("expected id 1, got %v", resp.ID)
	}
}

func TestServer_OversizedFrameRejected(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())

	// Build a frame larger than the 8 MB max frame size.
	large := make([]byte, maxFrameSize+1)
	for i := range large {
		large[i] = 'x'
	}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"padding":"` + string(large) + `"}}` + "\n"

	stdin := strings.NewReader(req)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error for oversized frame, got %v", resp.Error)
	}
}

func TestServer_InvalidWorkingDirRejected(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"workingDir":"/does/not/exist"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected invalid working directory error, got %v", resp.Error)
	}
}

func TestServer_SessionNewRestoresCwdOnError(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	tmpDir := t.TempDir()
	app := &fakeAppRunner{
		startSessionErr: errors.New("session failure"),
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"workingDir":"` + tmpDir + `"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after run: %v", err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("eval symlinks cwd: %v", err)
	}
	resolvedOrig, err := filepath.EvalSymlinks(origDir)
	if err != nil {
		t.Fatalf("eval symlinks orig: %v", err)
	}
	if resolvedCwd != resolvedOrig {
		t.Fatalf("cwd not restored: got %q want %q", resolvedCwd, resolvedOrig)
	}
}

func TestServer_LoadSessionChangesCwd(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	tmpDir := t.TempDir()
	app := &fakeAppRunner{
		resumeSessionReturn: &api.Session{ID: "sess-load", Path: tmpDir},
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"sess-load"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error != nil {
		t.Fatalf("unexpected load error: %v", resp.Error)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after run: %v", err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("eval symlinks cwd: %v", err)
	}
	resolvedTmp, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("eval symlinks tmp: %v", err)
	}
	if resolvedCwd != resolvedTmp {
		t.Fatalf("cwd not changed to session path: got %q want %q", resolvedCwd, resolvedTmp)
	}
}

func TestServer_ApprovalDiffForwarded(t *testing.T) {
	ch := make(chan api.TurnEvent, 2)
	ch <- api.TurnEvent{Type: api.TurnEventApprovalDiff, DiffCallID: "call-1", DiffContent: "diff-data"}
	ch <- api.TurnEvent{Type: api.TurnEventDone, Content: "done"}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-diff", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())

	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"diff"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), stdout.String())
	}

	var update jsonRPCNotification
	if err := json.Unmarshal([]byte(lines[1]), &update); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	if update.Method != "session/update" {
		t.Fatalf("expected session/update, got %s", update.Method)
	}
	params, _ := update.Params.(map[string]any)
	if params["sessionUpdate"] != "approval_diff" {
		t.Fatalf("unexpected update type: %v", params["sessionUpdate"])
	}
	if params["diffCallId"] != "call-1" {
		t.Fatalf("unexpected diff call id: %v", params["diffCallId"])
	}
	if params["diffContent"] != "diff-data" {
		t.Fatalf("unexpected diff content: %v", params["diffContent"])
	}
}

func TestNewServer_NilLogger(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, nil)
	if srv.logger == nil {
		t.Fatal("expected default logger when nil is passed")
	}
}

func TestServer_InvalidJSONRPCVersion(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"1.0","id":1,"method":"initialize"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected invalid request error, got %v", resp.Error)
	}
}

func TestServer_InitializeInvalidParams(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":"not-an-object"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected invalid params error, got %v", resp.Error)
	}
}

func TestServer_InitializeUnsupportedProtocolVersion(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":99}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected unsupported protocol version error, got %v", resp.Error)
	}
}

func TestServer_SessionNewInvalidParams(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":"bad"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected invalid params error, got %v", resp.Error)
	}
}

func TestServer_SessionNewStartSessionError(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	tmpDir := t.TempDir()
	app := &fakeAppRunner{
		startSessionErr: errors.New("session creation failed"),
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"workingDir":"` + tmpDir + `"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected session creation error, got %v", resp.Error)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after run: %v", err)
	}
	resolvedCwd, _ := filepath.EvalSymlinks(cwd)
	resolvedOrig, _ := filepath.EvalSymlinks(origDir)
	if resolvedCwd != resolvedOrig {
		t.Fatalf("cwd not restored after StartSession error: got %q want %q", resolvedCwd, resolvedOrig)
	}
}

func TestServer_SessionNewNotADirectory(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"workingDir":"` + tmpFile + `"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected invalid working directory error, got %v", resp.Error)
	}
}

func TestServer_SessionLoadResumeError(t *testing.T) {
	app := &fakeAppRunner{
		resumeSessionErr: errors.New("resume failed"),
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"missing"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected resume error, got %v", resp.Error)
	}
}

func TestServer_SessionLoadPathChangeError(t *testing.T) {
	app := &fakeAppRunner{
		resumeSessionReturn: &api.Session{ID: "sess-bad-path", Path: "/does/not/exist"},
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"sess-bad-path"}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected invalid session path error, got %v", resp.Error)
	}
}

func TestChangeWorkingDir_SameDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := changeWorkingDir(cwd, cwd); err != nil {
		t.Fatalf("changeWorkingDir same dir: %v", err)
	}
}

func TestChangeWorkingDir_ChdirError(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.Chmod(tmpDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() {
		if err := os.Chmod(tmpDir, 0755); err != nil {
			t.Fatalf("restore chmod: %v", err)
		}
	}()

	if err := changeWorkingDir(tmpDir, tmpDir); err == nil {
		t.Fatal("expected chdir error")
	}
}

func TestChangeWorkingDir_RejectsEscape(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := changeWorkingDir(subDir, tmpDir); err != nil {
		t.Fatalf("changeWorkingDir sub dir: %v", err)
	}
	if err := changeWorkingDir(tmpDir, subDir); err == nil {
		t.Fatal("expected error for directory outside allowed root")
	}
	// Empty allowed root disables validation.
	if err := changeWorkingDir(tmpDir, ""); err != nil {
		t.Fatalf("changeWorkingDir with empty root: %v", err)
	}
}

func TestServer_CancelNotification(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","method":"session/cancel"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("expected no response for notification, got %s", stdout.String())
	}
}

func TestServer_CancelNoPrompt(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/cancel"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["cancelled"] != false {
		t.Fatalf("expected cancelled=false, got %v", result["cancelled"])
	}
}

func TestServer_PromptEmpty(t *testing.T) {
	app := &fakeAppRunner{startSessionReturn: &api.Session{ID: "sess-empty", Path: "/tmp"}}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":""}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	resp := parseLine(t, lines[len(lines)-1])
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected empty prompt error, got %v", resp.Error)
	}
}

func TestServer_PromptInvalidParams(t *testing.T) {
	app := &fakeAppRunner{startSessionReturn: &api.Session{ID: "sess-bad-params", Path: "/tmp"}}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":"bad"}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	resp := parseLine(t, lines[len(lines)-1])
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected invalid params error, got %v", resp.Error)
	}
}

func TestServer_PromptRunTurnError(t *testing.T) {
	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-runturn-err", Path: "/tmp"},
		runTurnErr:         errors.New("run turn failed"),
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"go"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	resp := parseLine(t, lines[len(lines)-1])
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected run turn error, got %v", resp.Error)
	}
}

func TestServer_PromptTurnEventNilError(t *testing.T) {
	ch := make(chan api.TurnEvent, 2)
	ch <- api.TurnEvent{Type: api.TurnEventError, Error: nil}
	ch <- api.TurnEvent{Type: api.TurnEventDone, Content: "ok"}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-nil-err", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"x"}}` + "\n",
	)
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	resp := parseLine(t, lines[len(lines)-1])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["response"] != "ok" {
		t.Fatalf("unexpected response: %v", result["response"])
	}
}

func TestServer_PromptUnknownEventTypeIgnored(t *testing.T) {
	ch := make(chan api.TurnEvent, 2)
	ch <- api.TurnEvent{Type: api.TurnEventType(99)}
	ch <- api.TurnEvent{Type: api.TurnEventDone, Content: "ok"}
	close(ch)

	app := &fakeAppRunner{
		startSessionReturn: &api.Session{ID: "sess-unknown", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"prompt":"x"}}` + "\n",
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
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

// failingWriter returns an error after writing a configurable number of bytes.
type failingWriter struct {
	allowed int
	written int
	err     error
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.written >= f.allowed {
		return 0, f.err
	}
	f.written += len(p)
	return len(p), nil
}

func TestWriteError_ContextDone(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enc := json.NewEncoder(&bytes.Buffer{})
	if err := srv.writeError(ctx, enc, 1, -1, "msg", errors.New("cause")); err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestWriteError_EncodeError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}
	enc := json.NewEncoder(w)
	if err := srv.writeError(context.Background(), enc, 1, -1, "msg", errors.New("cause")); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestWriteError_NilCause(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	var stdout bytes.Buffer
	enc := json.NewEncoder(&stdout)
	if err := srv.writeError(context.Background(), enc, 1, -1, "msg", nil); err != nil {
		t.Fatalf("writeError: %v", err)
	}
	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Data != nil {
		t.Fatalf("expected nil error data, got %v", resp.Error)
	}
}

func TestWriteResult_ContextDone(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enc := json.NewEncoder(&bytes.Buffer{})
	if err := srv.writeResult(ctx, enc, 1, map[string]any{}); err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestWriteResult_EncodeError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}
	enc := json.NewEncoder(w)
	if err := srv.writeResult(context.Background(), enc, 1, map[string]any{}); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestWriteNotification_ContextDone(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enc := json.NewEncoder(&bytes.Buffer{})
	if err := srv.writeNotification(ctx, enc, "session/update", map[string]any{}); err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestWriteNotification_EncodeError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}
	enc := json.NewEncoder(w)
	if err := srv.writeNotification(context.Background(), enc, "session/update", map[string]any{}); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestServer_RunContextCancelled(t *testing.T) {
	app := &fakeAppRunner{}
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

	send(t, wStdin, "initialize", 1, map[string]any{})
	time.Sleep(50 * time.Millisecond)
	cancel()
	wStdin.Close()
	wg.Wait()

	if runErr == nil || (!errors.Is(runErr, context.Canceled) && !strings.Contains(runErr.Error(), "context canceled")) {
		t.Fatalf("expected context cancelled error, got %v", runErr)
	}
}

func TestServer_RunAppCloseError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	app := &fakeAppRunner{closeErr: errors.New("close failed")}
	srv := NewServer(app, logger)
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
	var stdout bytes.Buffer

	err := srv.Run(context.Background(), stdin, &stdout)
	if err != nil {
		t.Fatalf("expected nil run error, got %v", err)
	}
	if !strings.Contains(logBuf.String(), "app close failed") {
		t.Fatalf("expected app close error log, got %s", logBuf.String())
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestServer_RunReadError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := &errorReader{err: errors.New("read failed")}
	var stdout bytes.Buffer

	err := srv.Run(context.Background(), stdin, &stdout)
	if err == nil || !strings.Contains(err.Error(), "read stdin") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestServer_EmptyLineSkipped(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader("\n" + `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
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
}

func TestServer_OversizedFrameWriteError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())

	large := make([]byte, maxFrameSize+1)
	for i := range large {
		large[i] = 'x'
	}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"padding":"` + string(large) + `"}}` + "\n"

	stdin := strings.NewReader(req)
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}

	err := srv.Run(context.Background(), stdin, w)
	if err == nil || !strings.Contains(err.Error(), "write oversized frame error") {
		t.Fatalf("expected oversized frame write error, got %v", err)
	}
}

func TestServer_ParseErrorWriteError(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader("not json\n")
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}

	err := srv.Run(context.Background(), stdin, w)
	if err == nil || !strings.Contains(err.Error(), "write parse error") {
		t.Fatalf("expected parse error write failure, got %v", err)
	}
}

func TestServer_PromptGoroutineErrorLogged(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	app := &fakeAppRunner{}
	srv := NewServer(app, logger)
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"prompt":"hi"}}` + "\n")
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}

	if err := srv.Run(context.Background(), stdin, w); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(logBuf.String(), "prompt handler failed") {
		t.Fatalf("expected prompt handler error log, got %s", logBuf.String())
	}
}

func TestServer_SyncHandleErrorReturned(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"1.0","id":1,"method":"initialize"}` + "\n")
	w := &failingWriter{allowed: 0, err: errors.New("write failed")}

	err := srv.Run(context.Background(), stdin, w)
	if err == nil || !strings.Contains(err.Error(), "encode error response") {
		t.Fatalf("expected sync handle error, got %v", err)
	}
}

func TestServer_SessionLoadInvalidParams(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/load","params":"bad"}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected invalid params error, got %v", resp.Error)
	}
}

func TestServer_SessionLoadEmptySessionID(t *testing.T) {
	app := &fakeAppRunner{}
	srv := NewServer(app, slog.Default())
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":""}}` + "\n")
	var stdout bytes.Buffer

	if err := srv.Run(context.Background(), stdin, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	resp := parseLine(t, strings.TrimSpace(stdout.String()))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected empty session id error, got %v", resp.Error)
	}
}
