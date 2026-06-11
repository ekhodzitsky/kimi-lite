package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/sidebar"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNew(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, err := New(cfg, session, context.Background())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.config != cfg {
		t.Error("config not set")
	}
	if m.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", m.state)
	}
}

func TestInit(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command")
	}
}

func TestWindowResize(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(*Model)

	if model.width != 120 {
		t.Errorf("width = %d, want 120", model.width)
	}
	if model.height != 40 {
		t.Errorf("height = %d, want 40", model.height)
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "hello"})
	model := updated.(*Model)

	if model.state != api.TurnThinking {
		t.Errorf("state = %d, want TurnThinking", model.state)
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if model.messages[0].Content != "hello" {
		t.Errorf("message content = %q, want %q", model.messages[0].Content, "hello")
	}

	if cmd == nil {
		t.Fatal("expected command for SendMessageMsg")
	}
	msg := cmd()
	if _, ok := msg.(SendMessageMsg); !ok {
		t.Errorf("expected SendMessageMsg, got %T", msg)
	}
}

func TestCommandCompact(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/compact"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for CompactMsg")
	}
	msg := cmd()
	if _, ok := msg.(CompactMsg); !ok {
		t.Errorf("expected CompactMsg, got %T", msg)
	}
	if len(model.messages) != 1 {
		t.Errorf("messages length = %d, want 1", len(model.messages))
	}
}

func TestCommandClear(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(input.SendMsg{Content: "hello"})
	m.setState(api.TurnIdle)
	updated, cmd := m.Update(input.SendMsg{Content: "/clear"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for ClearMsg")
	}
	msg := cmd()
	if _, ok := msg.(ClearMsg); !ok {
		t.Errorf("expected ClearMsg, got %T", msg)
	}
	if len(model.messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(model.messages))
	}
}

func TestCommandSessions(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/sessions"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for SessionsMsg")
	}
	msg := cmd()
	if _, ok := msg.(SessionsMsg); !ok {
		t.Errorf("expected SessionsMsg, got %T", msg)
	}
	if len(model.messages) != 1 {
		t.Errorf("messages length = %d, want 1", len(model.messages))
	}
}

func TestCommandGoal(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/goal write tests"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for GoalMsg")
	}
	msg := cmd()
	goal, ok := msg.(GoalMsg)
	if !ok {
		t.Fatalf("expected GoalMsg, got %T", msg)
	}
	if goal.Content != "write tests" {
		t.Errorf("GoalMsg.Content = %q, want %q", goal.Content, "write tests")
	}
	if len(model.messages) != 1 {
		t.Errorf("messages length = %d, want 1", len(model.messages))
	}
}

func TestCommandBTW(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/btw remember this"})
	_ = updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for BTWMsg")
	}
	msg := cmd()
	btw, ok := msg.(BTWMsg)
	if !ok {
		t.Fatalf("expected BTWMsg, got %T", msg)
	}
	if btw.Content != "remember this" {
		t.Errorf("BTWMsg.Content = %q, want %q", btw.Content, "remember this")
	}
}

func TestCommandCheckpoint(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/checkpoint"})
	_ = updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for CheckpointMsg")
	}
	msg := cmd()
	if _, ok := msg.(CheckpointMsg); !ok {
		t.Errorf("expected CheckpointMsg, got %T", msg)
	}
}

func TestCommandUnknown(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/unknown"})
	model := updated.(*Model)

	if cmd != nil {
		t.Error("unknown command should not produce a message")
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if model.messages[0].Content != "unknown command: /unknown" {
		t.Errorf("error message = %q, want %q", model.messages[0].Content, "unknown command: /unknown")
	}
}

func TestStreamChunk(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Start a turn
	m.Update(input.SendMsg{Content: "hello"})

	// Receive stream chunk
	updated, _ := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "world"}})
	model := updated.(*Model)

	if model.state != api.TurnStreaming {
		t.Errorf("state = %d, want TurnStreaming", model.state)
	}
	if len(model.messages) != 2 {
		t.Fatalf("messages length = %d, want 2", len(model.messages))
	}
	if model.messages[1].Content != "world" {
		t.Errorf("assistant content = %q, want %q", model.messages[1].Content, "world")
	}
}

func TestStreamChunkDone(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(input.SendMsg{Content: "hello"})
	m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "done"}})

	updated, cmd := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Done: true}})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	if cmd != nil {
		t.Fatal("expected no command when done without tool calls")
	}
}

func TestStreamChunkError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Error: errors.New("stream failed")}})
	model := updated.(*Model)

	if model.state != api.TurnError {
		t.Errorf("state = %d, want TurnError", model.state)
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if !strings.Contains(model.messages[0].Content, "stream failed") {
		t.Errorf("error message = %q, want to contain 'stream failed'", model.messages[0].Content)
	}
}

func TestToolCall(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`},
	}
	updated, _ := m.Update(ToolCallMsg{Calls: calls})
	model := updated.(*Model)

	if model.state != api.TurnToolCalls {
		t.Errorf("state = %d, want TurnToolCalls", model.state)
	}
	if model.toolCount != 1 {
		t.Errorf("toolCount = %d, want 1", model.toolCount)
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if model.messages[0].Content != "" {
		t.Errorf("tool call message content should be empty, got %q", model.messages[0].Content)
	}
}

func TestToolResult(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{{ID: "1", Name: "read_file", Arguments: `{}`}}
	m.Update(ToolCallMsg{Calls: calls})

	result := api.ToolResult{CallID: "1", Name: "read_file", Output: "file content"}
	updated, _ := m.Update(ToolResultMsg{Result: result})
	model := updated.(*Model)

	found := false
	for _, msg := range model.messages {
		if msg.Type == 2 && msg.ToolResult != nil && msg.ToolResult.Output == "file content" { // TypeToolCall = 2
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result not found in messages")
	}
}

func TestApprovalRequest(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "edit_file", Arguments: `{}`},
	}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls})
	model := updated.(*Model)

	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}
	if len(model.pendingApprovals) != 1 {
		t.Errorf("pendingApprovals length = %d, want 1", len(model.pendingApprovals))
	}
}

func TestApprovalResponse(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "edit_file", Arguments: `{}`},
	}
	m.Update(ApprovalRequestMsg{Calls: calls})

	updated, _ := m.Update(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "1"})
	model := updated.(*Model)

	if model.state != api.TurnThinking {
		t.Errorf("state = %d, want TurnThinking", model.state)
	}
	if len(model.pendingApprovals) != 0 {
		t.Errorf("pendingApprovals length = %d, want 0", len(model.pendingApprovals))
	}
}

func TestStateChange(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	updated, _ := m.Update(StateChangeMsg{State: api.TurnThinking})
	model := updated.(*Model)

	if model.state != api.TurnThinking {
		t.Errorf("state = %d, want TurnThinking", model.state)
	}
}

func TestErrorMsg(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(ErrorMsg{Err: errors.New("something broke")})
	model := updated.(*Model)

	if model.state != api.TurnError {
		t.Errorf("state = %d, want TurnError", model.state)
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if !strings.Contains(model.messages[0].Content, "something broke") {
		t.Errorf("error message = %q, want to contain 'something broke'", model.messages[0].Content)
	}
}

func TestQuitKey(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command")
	}

	// Verify it's a quit command by executing it
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestCancelKey(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.setState(api.TurnThinking)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle after cancel", model.state)
	}
}

func TestToggleSidebar(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	visibleBefore := m.sidebar.Visible()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	model := updated.(*Model)

	if model.sidebar.Visible() == visibleBefore {
		t.Error("sidebar visibility should toggle")
	}
}

func TestSetters(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	m.SetModelName("gpt-4")
	if m.modelName != "gpt-4" {
		t.Errorf("modelName = %q, want %q", m.modelName, "gpt-4")
	}

	m.SetContextStats(100, 200)
	if m.contextUsed != 100 {
		t.Errorf("contextUsed = %d, want 100", m.contextUsed)
	}
	if m.contextMax != 200 {
		t.Errorf("contextMax = %d, want 200", m.contextMax)
	}

	m.SetToolCount(5)
	if m.toolCount != 5 {
		t.Errorf("toolCount = %d, want 5", m.toolCount)
	}

	newSession := &api.Session{ID: "new", Path: "/home"}
	m.SetSession(newSession)
	if m.session.ID != "new" {
		t.Errorf("session.ID = %q, want %q", m.session.ID, "new")
	}

	m.SetTurnManager(nil)
	if m.turnManager != nil {
		t.Error("turnManager should be nil")
	}

	m.SetSessionManager(nil)
	if m.sessionManager != nil {
		t.Error("sessionManager should be nil")
	}

	m.SetCompressor(nil)
	if m.compressor != nil {
		t.Error("compressor should be nil")
	}

	m.SetGitProvider(nil)
	if m.gitProvider != nil {
		t.Error("gitProvider should be nil")
	}

	m.SetMCPClient(nil)
	if m.mcpClient != nil {
		t.Error("mcpClient should be nil")
	}

	m.SetStore(nil)
	if m.store != nil {
		t.Error("store should be nil")
	}
}

func TestView(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	// Before resize, should show loading
	view := m.View()
	if view != "Loading..." {
		t.Errorf("View() = %q, want 'Loading...'", view)
	}

	// After resize
	m.width = 120
	m.height = 40
	m.updateLayout()
	view = m.View()
	if view == "" {
		t.Error("View() should not be empty after resize")
	}
	if !strings.Contains(view, m.config.LLM.Model) {
		t.Errorf("View() should contain model name %q", m.config.LLM.Model)
	}
}

func TestState(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	if m.State() != api.TurnIdle {
		t.Errorf("State() = %d, want TurnIdle", m.State())
	}

	m.setState(api.TurnThinking)
	if m.State() != api.TurnThinking {
		t.Errorf("State() = %d, want TurnThinking", m.State())
	}
}

func TestStatusBarStates(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	states := []api.TurnState{
		api.TurnIdle,
		api.TurnThinking,
		api.TurnStreaming,
		api.TurnToolCalls,
		api.TurnWaitingApproval,
		api.TurnError,
	}

	for _, s := range states {
		m.setState(s)
		view := m.statusBar()
		if view == "" {
			t.Errorf("statusBar() empty for state %d", s)
		}
	}
}

func TestStreamChunkWithToolCalls(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(input.SendMsg{Content: "hello"})
	updated, cmd := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{
		Done: true,
		ToolCalls: []api.ToolCall{
			{ID: "1", Name: "read_file", Arguments: `{}`},
		},
	}})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	if cmd == nil {
		t.Fatal("expected command for tool calls in done chunk")
	}
	msg := cmd()
	if _, ok := msg.(ToolCallMsg); !ok {
		t.Errorf("expected ToolCallMsg, got %T", msg)
	}
}

func TestSidebarSelectFile(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(sidebar.SelectFileMsg{Path: "/tmp/test.go"})
	model := updated.(*Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if !strings.Contains(model.messages[0].Content, "/tmp/test.go") {
		t.Errorf("message should contain file path, got %q", model.messages[0].Content)
	}
}

func TestErrorMsg_DoesNotOverrideTurnErrorOnDone(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Simulate real flow: ErrorMsg from readStreamChunk sets TurnError.
	updated, _ := m.Update(ErrorMsg{Err: errors.New("stream broke")})
	model := updated.(*Model)

	if model.state != api.TurnError {
		t.Fatalf("state = %d, want TurnError", model.state)
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model.messages))
	}
	if !strings.Contains(model.messages[0].Content, "stream broke") {
		t.Errorf("error message = %q, want to contain 'stream broke'", model.messages[0].Content)
	}

	// A subsequent StreamChunkMsg{Done: true} (e.g. from channel close after error)
	// must NOT flip the state back to TurnIdle.
	updated2, _ := model.Update(StreamChunkMsg{Chunk: api.StreamChunk{Done: true}})
	model2 := updated2.(*Model)

	if model2.state != api.TurnError {
		t.Errorf("state after Done = %d, want TurnError", model2.state)
	}
}
