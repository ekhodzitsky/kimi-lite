package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/llm"
	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type testConfigProvider struct {
	cfg *api.Config
}

func (p *testConfigProvider) Get() *api.Config {
	return p.cfg
}

type chatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// TestAgenticLoop drives a full user-input -> LLM -> tool-call -> tool-result ->
// final-response cycle using real store, real executor, real TurnManager, and an
// httptest-backed LLM client.
func TestAgenticLoop(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a file for the read_file tool to read.
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello from integration test"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// The executor interprets relative paths against the current working
	// directory, so run the test from inside the sandbox.
	t.Chdir(tmpDir)

	// SQLite store on disk so the full persistence path is exercised.
	dbPath := filepath.Join(tmpDir, "sessions.db")
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	session, err := st.CreateSession(ctx, tmpDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// LLM server that first requests a read_file tool call, then returns a
	// final answer once it sees the tool result message.
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		lastRole := ""
		if len(req.Messages) > 0 {
			lastRole = req.Messages[len(req.Messages)-1].Role
		}

		if lastRole == "tool" {
			// Send content in a non-terminal chunk so the client forwards it,
			// then a separate stop chunk.
			writeSSE(w, `{"choices":[{"delta":{"content":"Done reading."},"finish_reason":""}]}`)
			writeSSE(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		} else {
			writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"test.txt\"}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		writeSSE(w, "[DONE]")
	}))
	defer llmServer.Close()

	llmCfg := api.LLMConfig{
		Provider: "openai-compatible",
		APIKey:   "test-key",
		Model:    "test-model",
		BaseURL:  llmServer.URL,
		Timeout:  10 * time.Second,
	}
	llmClient := llm.NewClient(llmCfg, &http.Client{Timeout: 10 * time.Second})

	// Built-in tool executor sandboxed to the temp directory.
	executor, err := core.NewBuiltInToolExecutor(core.ToolExecutorConfig{
		SandboxRoot:  tmpDir,
		ShellTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}
	defer executor.Close()

	cfg := &api.Config{
		Behavior: api.BehaviorConfig{
			AutoApprove:   []string{"read_file"},
			MaxTurns:      10,
			MaxToolRounds: 10,
		},
	}
	approval := core.NewApprovalGate(core.ModeAuto, cfg.Behavior.AutoApprove, executor.IsReadOnly, nil)

	turnMgr := core.NewTurnManager(llmClient, executor, approval, st, &testConfigProvider{cfg: cfg})

	// Run the turn.
	events, err := turnMgr.RunTurn(ctx, session.ID, "please read the file")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	var contents []string
	var toolResults []api.ToolResult
	var sawDone bool
	for e := range events {
		switch e.Type {
		case api.TurnEventContent:
			contents = append(contents, e.Content)
		case api.TurnEventToolResult:
			toolResults = append(toolResults, e.Result)
		case api.TurnEventDone:
			sawDone = true
		case api.TurnEventError:
			t.Fatalf("unexpected error event: %v", e.Error)
		}
	}

	if !sawDone {
		t.Fatal("expected TurnEventDone")
	}
	if got := strings.Join(contents, ""); got != "Done reading." {
		t.Errorf("final content = %q, want %q", got, "Done reading.")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResults))
	}
	if !strings.Contains(toolResults[0].Output, "hello from integration test") {
		t.Errorf("tool result missing file content: %+v", toolResults[0])
	}

	// Verify persisted messages.
	msgs, err := st.GetMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}

	var userFound, assistantToolFound, toolFound, assistantFinalFound bool
	for _, msg := range msgs {
		switch msg.Role {
		case api.RoleUser:
			userFound = true
		case api.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				assistantToolFound = true
				if msg.ToolCalls[0].Name != "read_file" {
					t.Errorf("expected read_file tool call, got %q", msg.ToolCalls[0].Name)
				}
				if msg.ToolCalls[0].Arguments == "" {
					t.Error("tool call arguments survived round-trip empty")
				}
			} else {
				assistantFinalFound = true
			}
		case api.RoleTool:
			toolFound = true
			if !strings.Contains(msg.Content, "hello from integration test") {
				t.Errorf("tool message missing file content: %q", msg.Content)
			}
		}
	}
	if !userFound {
		t.Error("missing persisted user message")
	}
	if !assistantToolFound {
		t.Error("missing persisted assistant tool-call message")
	}
	if !toolFound {
		t.Error("missing persisted tool result message")
	}
	if !assistantFinalFound {
		t.Error("missing persisted final assistant message")
	}

	// Verify the persisted turn.
	turns, err := st.GetTurns(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("GetTurns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].State != api.TurnIdle {
		t.Errorf("turn state = %v, want TurnIdle", turns[0].State)
	}
	if !strings.Contains(turns[0].Response, "Done reading.") {
		t.Errorf("turn response = %q, want final answer", turns[0].Response)
	}
}

func writeSSE(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
