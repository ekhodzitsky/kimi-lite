package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeLLM is a deterministic api.LLMClient for integration tests.
// It returns a fixed response on the first ChatStream call and, optionally,
// a tool-call response before the final text response.
type fakeLLM struct {
	mu        sync.Mutex
	responses []fakeResponse
	calls     int
}

type fakeResponse struct {
	content   string
	toolCalls []api.ToolCall
}

func (f *fakeLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &api.Message{Role: api.RoleAssistant, Content: "fake"}, nil
}

func (f *fakeLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ch := make(chan api.StreamChunk, 1)
	idx := f.calls
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	resp := f.responses[idx]
	f.calls++
	ch <- api.StreamChunk{
		Content:   resp.content,
		ToolCalls: resp.toolCalls,
		Done:      true,
	}
	close(ch)
	return ch, nil
}

func (f *fakeLLM) Models() []api.ModelInfo { return nil }

type staticConfig struct {
	cfg *api.Config
}

func (s *staticConfig) Get() *api.Config { return s.cfg }

func TestTurnLoop_ReadFile(t *testing.T) {
	defer goleak.VerifyNone(t)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "smoke.db")

	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	session, err := st.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := st.AppendMessage(context.Background(), session.ID, api.Message{
		Role:    api.RoleSystem,
		Content: "You are a helpful assistant.",
	}); err != nil {
		t.Fatalf("append system message: %v", err)
	}

	targetFile := filepath.Join(tmpDir, "hello.txt")
	if err := writeTestFile(t, targetFile, "hello world"); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	cfg := &api.Config{}
	llm := &fakeLLM{
		responses: []fakeResponse{
			{
				toolCalls: []api.ToolCall{
					{
						ID:        "call-1",
						Name:      "read_file",
						Arguments: fmt.Sprintf(`{"path":%q}`, targetFile),
					},
				},
			},
			{content: "I read the file."},
		},
	}

	tools, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		SandboxRoot:  tmpDir,
		ShellTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	defer tools.Close()

	approval := core.NewApprovalGate(
		core.ModeAuto,
		[]string{"read_file"},
		tools.IsReadOnly,
		nil,
	)

	tm, err := core.NewTurnManager(llm, tools, approval, st, &staticConfig{cfg: cfg})
	if err != nil {
		t.Fatalf("create turn manager: %v", err)
	}

	events, err := tm.RunTurn(context.Background(), session.ID, "read hello.txt")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sawContent, sawToolResult bool
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				if !sawContent {
					t.Fatal("never saw assistant content event")
				}
				if !sawToolResult {
					t.Fatal("never saw tool result event")
				}
				return
			}
			switch ev.Type {
			case api.TurnEventContent:
				if ev.Content != "" {
					sawContent = true
				}
			case api.TurnEventToolResult:
				if ev.Result.Name == "read_file" && ev.Result.Output == "hello world" {
					sawToolResult = true
				}
			case api.TurnEventError:
				t.Fatalf("unexpected turn error: %v", ev.Error)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for turn events")
		}
	}
}

func writeTestFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o644)
}

// blockingFakeLLM blocks in ChatStream until its context is cancelled. It is used
// to hold a TurnManager in the running state so concurrent RunTurn calls can be
// rejected.
type blockingFakeLLM struct {
	mu       sync.Mutex
	started  chan struct{}
	cancelFn context.CancelFunc
}

func (f *blockingFakeLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *blockingFakeLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	f.mu.Lock()
	ctx, f.cancelFn = context.WithCancel(ctx)
	started := f.started
	f.mu.Unlock()
	close(started)

	ch := make(chan api.StreamChunk)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (f *blockingFakeLLM) Models() []api.ModelInfo { return nil }

func TestRunTurn_RejectsConcurrentCalls(t *testing.T) {
	defer goleak.VerifyNone(t)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "concurrent.db")

	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer st.Close()

	session, err := st.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	llm := &blockingFakeLLM{started: make(chan struct{})}
	approval := core.NewApprovalGate(core.ModeAuto, nil, func(string) bool { return false }, nil)
	tools, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		SandboxRoot:  tmpDir,
		ShellTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	defer tools.Close()

	tm, err := core.NewTurnManager(llm, tools, approval, st, &staticConfig{cfg: &api.Config{}})
	if err != nil {
		t.Fatalf("create turn manager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := tm.RunTurn(ctx, session.ID, "first prompt")
	if err != nil {
		t.Fatalf("first RunTurn: %v", err)
	}
	defer func() {
		cancel()
		for range events {
		}
	}()

	select {
	case <-llm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking LLM did not start")
	}

	_, err = tm.RunTurn(ctx, session.ID, "second prompt")
	if err == nil {
		t.Fatal("expected concurrent RunTurn to be rejected")
	}
}
