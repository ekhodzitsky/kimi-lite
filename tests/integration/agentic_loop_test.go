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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a file for the read_file tool to read.
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello from integration test"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

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
		defer r.Body.Close()

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

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
			writeSSE(w, fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"%s\"}"}}]},"finish_reason":"tool_calls"}]}`, testFile))
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

	httpClient := &http.Client{Timeout: 10 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)
	llmClient := llm.NewClient(llmCfg, httpClient)

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

	if len(msgs) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d: %+v", len(msgs), msgs)
	}

	if msgs[0].Role != api.RoleUser {
		t.Errorf("message[0].role = %q, want %q", msgs[0].Role, api.RoleUser)
	}
	if msgs[0].Content != "please read the file" {
		t.Errorf("message[0].content = %q, want user prompt", msgs[0].Content)
	}

	if msgs[1].Role != api.RoleAssistant {
		t.Errorf("message[1].role = %q, want %q", msgs[1].Role, api.RoleAssistant)
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on assistant message, got %d", len(msgs[1].ToolCalls))
	}
	if msgs[1].ToolCalls[0].Name != "read_file" {
		t.Errorf("tool call name = %q, want read_file", msgs[1].ToolCalls[0].Name)
	}
	wantArgs := fmt.Sprintf(`{"path":"%s"}`, testFile)
	if msgs[1].ToolCalls[0].Arguments != wantArgs {
		t.Errorf("tool call arguments = %q, want %q (SQLite round-trip)", msgs[1].ToolCalls[0].Arguments, wantArgs)
	}

	if msgs[2].Role != api.RoleTool {
		t.Errorf("message[2].role = %q, want %q", msgs[2].Role, api.RoleTool)
	}
	if msgs[2].ToolCallID != "call_1" {
		t.Errorf("tool message tool_call_id = %q, want call_1", msgs[2].ToolCallID)
	}
	if !strings.Contains(msgs[2].Content, "hello from integration test") {
		t.Errorf("tool message missing file content: %q", msgs[2].Content)
	}

	if msgs[3].Role != api.RoleAssistant {
		t.Errorf("message[3].role = %q, want %q", msgs[3].Role, api.RoleAssistant)
	}
	if msgs[3].Content != "Done reading." {
		t.Errorf("final assistant message = %q, want %q", msgs[3].Content, "Done reading.")
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
