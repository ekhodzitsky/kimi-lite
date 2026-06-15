package tests

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/observability"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// cyclicFakeLLM alternates between a tool-call response and a final content
// response on successive calls.
type cyclicFakeLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *cyclicFakeLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &api.Message{Role: api.RoleAssistant, Content: "fake"}, nil
}

func (f *cyclicFakeLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ch := make(chan api.StreamChunk, 1)

	call := f.calls
	f.calls++

	if call%2 == 0 {
		ch <- api.StreamChunk{
			ToolCalls: []api.ToolCall{{
				ID:        fmt.Sprintf("call-%d", call),
				Name:      "read_file",
				Arguments: `{"path":"hello.txt"}`,
			}},
			Done: true,
		}
	} else {
		ch <- api.StreamChunk{Content: "done", Done: true}
	}
	close(ch)
	return ch, nil
}

func (f *cyclicFakeLLM) Models() []api.ModelInfo { return nil }

func TestLongRunningTurns_NoLeaks(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "long.db")

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

	if err := writeTestFile(t, filepath.Join(tmpDir, "hello.txt"), "hello world"); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	cfg := &api.Config{}
	llm := &cyclicFakeLLM{}
	tools, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		SandboxRoot:  tmpDir,
		ShellTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	t.Cleanup(func() { _ = tools.Close() })

	approval := core.NewApprovalGate(
		core.ModeAuto,
		[]string{"read_file"},
		tools.IsReadOnly,
		nil,
	)

	metrics := observability.NewCollector()
	tm, err := core.NewTurnManager(llm, tools, approval, st, &staticConfig{cfg: cfg})
	if err != nil {
		t.Fatalf("create turn manager: %v", err)
	}
	tm.SetMetricsCollector(metrics)
	t.Cleanup(func() {
		tm.CancelAll()
		tm.Wait()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turns := 25
	for i := 0; i < turns; i++ {
		events, err := tm.RunTurn(ctx, session.ID, fmt.Sprintf("turn %d", i))
		if err != nil {
			t.Fatalf("run turn %d: %v", i, err)
		}

		sawContent := false
		for ev := range events {
			switch ev.Type {
			case api.TurnEventError:
				t.Fatalf("turn %d error: %v", i, ev.Error)
			case api.TurnEventContent:
				if ev.Content != "" {
					sawContent = true
				}
			}
		}
		if !sawContent {
			t.Fatalf("turn %d produced no assistant content", i)
		}
	}

	msgs, err := st.GetMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	// system + turns*(user + assistant tool-call + tool result + assistant text)
	wantMessages := 1 + turns*4
	if len(msgs) != wantMessages {
		t.Errorf("messages = %d, want %d", len(msgs), wantMessages)
	}

	if metrics.CounterValue("turn.completed") != int64(turns) {
		t.Errorf("turn.completed = %d, want %d", metrics.CounterValue("turn.completed"), turns)
	}
}
