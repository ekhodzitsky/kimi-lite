package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeLLM is a deterministic api.LLMClient for integration tests.
// It returns a fixed response on the first ChatStream call and, optionally,
// a tool-call response before the final text response.
type fakeLLM struct {
	responses []fakeResponse
	calls     int
}

type fakeResponse struct {
	content   string
	toolCalls []api.ToolCall
}

func (f *fakeLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	return &api.Message{Role: api.RoleAssistant, Content: "fake"}, nil
}

func (f *fakeLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
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
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "smoke.db")

	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

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

	approval := core.NewApprovalGate(
		core.ModeAuto,
		[]string{"read_file"},
		func(name string) bool { return name == "read_file" },
		nil,
	)
	tools, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		SandboxRoot:  tmpDir,
		ShellTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	tm := core.NewTurnManager(llm, tools, approval, st, &staticConfig{cfg: cfg})

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
	return os.WriteFile(path, []byte(content), 0o600)
}
