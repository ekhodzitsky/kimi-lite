package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
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

type recordingCompressor struct {
	gotKeepRecent int
}

func (c *recordingCompressor) Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error) {
	c.gotKeepRecent = keepRecent
	return 3, nil
}

func TestCommandCompact_KeepRecentFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Behavior.CompactKeepRecent = 7
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	comp := &recordingCompressor{}
	m.SetCompressor(comp)
	m.SetStore(&mockStore{})

	updated, cmd := m.Update(input.SendMsg{Content: "/compact"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for compact")
	}
	msg := cmd()
	if _, ok := msg.(CompactResultMsg); !ok {
		t.Fatalf("expected CompactResultMsg, got %T", msg)
	}
	if comp.gotKeepRecent != 7 {
		t.Errorf("Compact keepRecent = %d, want 7", comp.gotKeepRecent)
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
		t.Fatal("expected command for sessions")
	}
	msg := cmd()
	if _, ok := msg.(SessionsResultMsg); !ok {
		t.Errorf("expected SessionsResultMsg, got %T", msg)
	}
	if len(model.messages) != 1 {
		t.Errorf("messages length = %d, want 1", len(model.messages))
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
		t.Fatal("expected command for checkpoint")
	}
	msg := cmd()
	if _, ok := msg.(CheckpointResultMsg); !ok {
		t.Errorf("expected CheckpointResultMsg, got %T", msg)
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
	if len(model.approval.pending()) != 1 {
		t.Errorf("pending approvals length = %d, want 1", len(model.approval.pending()))
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
	if len(model.approval.pending()) != 0 {
		t.Errorf("pending approvals length = %d, want 0", len(model.approval.pending()))
	}
}

func TestApprovalResponse_DiffDoesNotFinalize(t *testing.T) {
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

	updated, _ := m.Update(ApprovalResponseMsg{Decision: api.ApprovalDiff, CallID: "1"})
	model := updated.(*Model)

	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}
	if len(model.approval.pending()) != 1 {
		t.Errorf("pending approvals length = %d, want 1", len(model.approval.pending()))
	}
	if model.approval.currentIndex() != 0 {
		t.Errorf("currentIndex = %d, want 0", model.approval.currentIndex())
	}
}

type recordingTurnManager struct {
	resumeCalls []struct {
		requestID int64
		approvals map[string]api.ApprovalDecision
	}
}

func (r *recordingTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (r *recordingTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	r.resumeCalls = append(r.resumeCalls, struct {
		requestID int64
		approvals map[string]api.ApprovalDecision
	}{requestID: requestID, approvals: approvals})
	return nil
}

func TestApprovalResponse_DiffForwardsToTurnManager(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	rtm := &recordingTurnManager{}
	m.SetTurnManager(rtm)

	calls := []api.ToolCall{
		{ID: "1", Name: "edit_file", Arguments: `{}`},
	}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 42})

	updated, cmd := m.Update(ApprovalResponseMsg{Decision: api.ApprovalDiff, CallID: "1"})
	model := updated.(*Model)

	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}

	if cmd == nil {
		t.Fatal("expected command to forward diff request")
	}

	// The returned command is a batch containing the ResumeWithApproval command
	// and the stream-polling command. Execute the batch to trigger the resume.
	batchMsg := cmd()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batchMsg)
	}
	var stateChangeSeen bool
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, ok := c().(StateChangeMsg); ok {
			stateChangeSeen = true
		}
	}
	if !stateChangeSeen {
		t.Error("expected a StateChangeMsg in the batch")
	}

	if len(rtm.resumeCalls) != 1 {
		t.Fatalf("expected 1 ResumeWithApproval call, got %d", len(rtm.resumeCalls))
	}
	if rtm.resumeCalls[0].requestID != 42 {
		t.Errorf("requestID = %d, want 42", rtm.resumeCalls[0].requestID)
	}
	if rtm.resumeCalls[0].approvals["1"] != api.ApprovalDiff {
		t.Errorf("approvals[1] = %v, want ApprovalDiff", rtm.resumeCalls[0].approvals["1"])
	}
}

func TestApprovalDiffMsg_AddsMessageAndKeepsWaiting(t *testing.T) {
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

	updated, _ := m.Update(ApprovalDiffMsg{CallID: "1", Diff: "--- a\n+++ a\n@@\n-old\n+new"})
	model := updated.(*Model)

	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}
	if len(model.approval.pending()) != 1 {
		t.Errorf("pending approvals length = %d, want 1", len(model.approval.pending()))
	}
	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "Diff preview") && strings.Contains(msg.Content, "-old") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diff preview message, got %+v", model.messages)
	}
}

func TestApprovalDialog_WideRunes(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: t.TempDir()}
	m, _ := New(cfg, session, context.Background())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Build a background containing CJK characters so the overlay must handle
	// wide runes outside the dialog as well as inside it.
	m.addMessage(msgcomp.NewUserMessage("背景に広い文字があります", m.styles))

	calls := []api.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path": "文件.txt"}`},
	}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	view := model.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Errorf("view line count = %d, want %d", len(lines), model.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > model.width {
			t.Errorf("line %d width = %d, want <= %d", i, w, model.width)
		}
	}
	if !strings.Contains(view, "文件.txt") {
		t.Errorf("view should contain wide-rune path, got %q", view)
	}
}

func TestApprovalDialog_NarrowTerminal(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: t.TempDir()}
	m, _ := New(cfg, session, context.Background())
	m.width = 30
	m.height = 10
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path": "/very/long/path/that/exceeds/terminal/width/file.txt"}`},
	}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	view := model.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Errorf("view line count = %d, want %d", len(lines), model.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > model.width {
			t.Errorf("line %d width = %d, want <= %d", i, w, model.width)
		}
	}
	if !strings.Contains(view, "Tool call requires approval") {
		t.Errorf("view should contain approval dialog, got %q", view)
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

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
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

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle after cancel", model.state)
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
	view := m.View().Content
	if view != "Loading..." {
		t.Errorf("View() = %q, want 'Loading...'", view)
	}

	// After resize
	m.width = 120
	m.height = 40
	m.updateLayout()
	view = m.View().Content
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

func TestWelcomeVisibleWhenEmpty(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 80
	m.height = 24
	m.updateLayout()

	view := m.View().Content
	if !strings.Contains(view, "Welcome to Kimi Code!") {
		t.Errorf("empty transcript should show welcome title, got:\n%s", view)
	}
	if !strings.Contains(view, "Directory:") {
		t.Errorf("welcome should show directory label, got:\n%s", view)
	}
	if !strings.Contains(view, "Session:") {
		t.Errorf("welcome should show session label, got:\n%s", view)
	}
	if !strings.Contains(view, "Model:") {
		t.Errorf("welcome should show model label, got:\n%s", view)
	}
	if !strings.Contains(view, "Version:") {
		t.Errorf("welcome should show version label, got:\n%s", view)
	}

	// The viewport should be shorter than the full available height because the
	// welcome panel occupies the top rows.
	if m.welcomeHeight() == 0 {
		t.Error("welcomeHeight should be non-zero when there are no messages")
	}
}

func TestWelcomeHiddenWhenMessages(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 80
	m.height = 24
	m.updateLayout()

	emptyHeight := m.layout().vpHeight

	m.addMessage(msgcomp.NewUserMessage("hello", m.styles))
	m.updateLayout()

	view := m.View().Content
	if strings.Contains(view, "Welcome to Kimi Code!") {
		t.Errorf("welcome panel should be hidden when messages exist, got:\n%s", view)
	}
	if m.welcomeHeight() != 0 {
		t.Errorf("welcomeHeight should be 0 with messages, got %d", m.welcomeHeight())
	}
	if m.layout().vpHeight <= emptyHeight {
		t.Errorf("viewport should grow when welcome is hidden: got %d, want > %d", m.layout().vpHeight, emptyHeight)
	}
}

func TestFooterStates(t *testing.T) {
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
		m.updateFooter()
		view := m.footer.View()
		if view == "" {
			t.Errorf("footer.View() empty for state %d", s)
		}
	}
}

func TestFooter_ShowsContextUsageAfterTurn(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.UI.ShowTokenCount = true
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetContextStats(0, 1000)

	// Simulate a completed assistant turn that added a message with content.
	m.messages = append(m.messages, &msgcomp.Message{
		Type:    msgcomp.TypeAssistant,
		Content: strings.Repeat("word ", 100), // 500 chars -> ~125 tokens
	})

	m.updateContextStats()
	m.updateFooter()

	bar := m.footer.View()
	if !strings.Contains(bar, "context:") {
		t.Fatalf("footer should show context usage, got %q", bar)
	}
	// The percentage must be non-zero after a non-empty turn.
	if strings.Contains(bar, "context: 0.0%") {
		t.Errorf("footer should show non-zero context usage, got %q", bar)
	}
}

func TestFooter_ShowsContextUsageRegardlessOfTokenCountSetting(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.UI.ShowTokenCount = false
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetContextStats(50, 100)
	m.updateFooter()

	bar := m.footer.View()
	if !strings.Contains(bar, "context:") {
		t.Errorf("footer should show context usage regardless of ShowTokenCount, got %q", bar)
	}
}

func TestFooter_TruncatesLongStatusOnNarrowTerminal(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 50
	m.height = 10
	m.updateLayout()
	m.statusText = strings.Repeat("x", 200)
	m.updateFooter()

	bar := m.footer.View()
	for _, line := range strings.Split(bar, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("footer line width = %d, want <= %d", w, m.width)
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

func TestTurnWithToolCallRendersToolCallAndResult(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	tm := &fakeTurnManager{
		// Stream goes straight to a tool call with no prose before it, then the
		// result, then follow-up prose. This exercises the path where there is no
		// pre-tool assistant message to trigger a full transcript rebuild.
		events: []api.TurnEvent{
			{Type: api.TurnEventDone, ToolCalls: []api.ToolCall{
				{ID: "call-1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`},
			}},
			{Type: api.TurnEventToolResult, Result: api.ToolResult{
				CallID: "call-1", Name: "read_file", Output: "file content",
			}},
			{Type: api.TurnEventContent, Content: "Done"},
			{Type: api.TurnEventDone},
		},
	}
	m.SetTurnManager(tm)

	updated, cmd := m.Update(input.SendMsg{Content: "read a file"})
	model := updated.(*Model)

	if model.state != api.TurnThinking {
		t.Fatalf("state = %d, want TurnThinking", model.state)
	}

	var toolCallMsg *msgcomp.Message
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		updated, cmd = model.Update(msg)
		model = updated.(*Model)

		if _, ok := msg.(ToolResultMsg); ok {
			for _, m := range model.messages {
				if m.Type == msgcomp.TypeToolCall && m.ToolCall.ID == "call-1" {
					toolCallMsg = m
					break
				}
			}
			if toolCallMsg == nil {
				t.Fatal("tool call message not found after tool result")
			}
			if toolCallMsg.ToolResult == nil {
				t.Fatal("tool result not attached to tool call message")
			}
			if toolCallMsg.ToolResult.Output != "file content" {
				t.Errorf("tool result output = %q, want %q", toolCallMsg.ToolResult.Output, "file content")
			}
			// The result should be visible immediately, before any follow-up content.
			view := model.vp.View().Content
			if strings.Contains(view, "pending") {
				t.Errorf("viewport should not contain pending status after result, got %q", view)
			}
			if !strings.Contains(toolCallMsg.View().Content, "Used") {
				t.Errorf("tool call message should render Used status, got %q", toolCallMsg.View().Content)
			}
		}
	}

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}

	if toolCallMsg == nil {
		t.Fatal("tool call message not found")
	}

	view := model.vp.View().Content
	if !strings.Contains(view, "read_file") {
		t.Errorf("viewport should contain tool name, got %q", view)
	}
	if !strings.Contains(toolCallMsg.View().Content, "Used") {
		t.Errorf("tool call message should render Used status, got %q", toolCallMsg.View().Content)
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

func TestYoloToggle(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	var modes []int
	m.SetApprovalModeSetter(func(mode int) {
		modes = append(modes, mode)
	})

	// First press: Auto -> Yolo
	m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if m.approvalMode != 2 {
		t.Errorf("approvalMode = %d, want 2 (ModeYolo)", m.approvalMode)
	}
	if len(modes) != 1 || modes[0] != 2 {
		t.Errorf("modes = %v, want [2]", modes)
	}

	// Second press: Yolo -> Auto
	m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if m.approvalMode != 1 {
		t.Errorf("approvalMode = %d, want 1 (ModeAuto)", m.approvalMode)
	}
	if len(modes) != 2 || modes[1] != 1 {
		t.Errorf("modes = %v, want [2, 1]", modes)
	}
}

func TestYoloToggle_Footer(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Default footer should not contain YOLO.
	m.updateFooter()
	view := m.footer.View()
	if strings.Contains(view, "YOLO") {
		t.Error("footer should not contain YOLO in default mode")
	}

	// Toggle to YOLO mode.
	m.SetApprovalMode(2)
	m.updateFooter()
	view = m.footer.View()
	if !strings.Contains(view, "YOLO") {
		t.Error("footer should contain YOLO when in yolo mode")
	}
}

func TestDirtyFlag_NavigationDoesNotRefreshViewport(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Adding a message sets dirty and triggers refresh
	m.addMessage(msgcomp.NewUserMessage("hello", m.styles))
	if !m.rb.isDirty() {
		t.Fatal("dirty should be true after adding a message")
	}

	// Process an Update to clear dirty
	updated, _ := m.Update(StateChangeMsg{State: api.TurnIdle})
	model := updated.(*Model)
	if model.rb.isDirty() {
		t.Fatal("dirty should be false after Update refreshes viewport")
	}

	// Navigation key should NOT set dirty and should NOT refresh viewport
	updated2, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model2 := updated2.(*Model)
	if model2.rb.isDirty() {
		t.Error("dirty should remain false after navigation key")
	}
}

func TestLayoutGeometryConsistency(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Add a message to hide the welcome panel so viewport geometry matches the
	// classic full-height layout.
	m.addMessage(msgcomp.NewUserMessage("hello", m.styles))
	m.updateLayout()

	// Compute expected layout values
	l := m.layout()

	// Click at vpHeight boundary should focus input
	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 50, Y: l.vpHeight})
	model := updated.(*Model)
	if model.focused != focusInput {
		t.Errorf("click at vpHeight boundary should focus input, got %d", model.focused)
	}

	// Click just above should focus viewport
	updated2, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 50, Y: l.vpHeight - 1})
	model2 := updated2.(*Model)
	if model2.focused != focusViewport {
		t.Errorf("click just above vpHeight should focus viewport, got %d", model2.focused)
	}
}

func TestRawModeToggle_FocusedMessagePath(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	assistant := msgcomp.NewAssistantMessage("**bold**", m.styles)
	m.addMessage(assistant)

	// Process an Update to clear dirty state and render the assistant message.
	updated, _ := m.Update(StateChangeMsg{State: api.TurnIdle})
	model := updated.(*Model)

	// Focus the viewport and press "r".
	model.focused = focusViewport
	updated2, _ := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model2 := updated2.(*Model)

	if len(model2.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model2.messages))
	}
	if !model2.messages[0].RawMode {
		t.Error("assistant message RawMode should be true after pressing r in viewport focus")
	}
}

func TestRawModeToggle_InputFocusIgnored(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	assistant := msgcomp.NewAssistantMessage("**bold**", m.styles)
	m.addMessage(assistant)

	updated, _ := m.Update(StateChangeMsg{State: api.TurnIdle})
	model := updated.(*Model)

	// Input is focused by default; pressing "r" should not toggle raw mode.
	updated2, _ := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model2 := updated2.(*Model)

	if len(model2.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(model2.messages))
	}
	if model2.messages[0].RawMode {
		t.Error("assistant message RawMode should remain false when input is focused")
	}
}

func TestDirtyFlag_StreamChunkSetsDirty(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Start a turn (adds user message and sets dirty, then Update clears it)
	updated, _ := m.Update(input.SendMsg{Content: "hello"})
	model := updated.(*Model)

	// Clear any remaining dirty state and capture viewport
	updated, _ = model.Update(StateChangeMsg{State: api.TurnIdle})
	model = updated.(*Model)

	// Stream chunk should trigger a viewport refresh
	updated2, _ := model.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "world"}})
	model2 := updated2.(*Model)
	view := model2.vp.View().Content
	if !strings.Contains(view, "world") {
		t.Errorf("viewport view should contain stream chunk content, got %q", view)
	}
	if model2.rb.isDirty() {
		t.Error("dirty should be false after Update refreshes viewport")
	}
}

func TestStreamChunk_StaleAfterCancellation(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Simulate starting a real turn: set streaming state and channel.
	m.setState(api.TurnStreaming)
	m.streamCh = make(<-chan api.TurnEvent)

	// Simulate cancellation (as handleKeyMsg does).
	m.mu.Lock()
	m.streamCh = nil
	m.streamCancel = nil
	m.streamCanceled = true
	m.mu.Unlock()
	m.setState(api.TurnIdle)

	// Deliver a stale buffered content chunk.
	updated, _ := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "stale"}})
	model := updated.(*Model)

	// No phantom assistant message should be created.
	if len(model.messages) != 0 {
		t.Fatalf("expected 0 messages after ignoring stale chunk, got %d", len(model.messages))
	}
	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
}

type slowCompressor struct{}

func (c *slowCompressor) Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

type slowSessionManager struct{}

func (s *slowSessionManager) CurrentSessionID() string { return "test" }
func (s *slowSessionManager) ClearMessages(ctx context.Context, id string) error {
	<-ctx.Done()
	return ctx.Err()
}
func (s *slowSessionManager) Rename(ctx context.Context, id string, name string) error {
	<-ctx.Done()
	return ctx.Err()
}
func (s *slowSessionManager) Fork(ctx context.Context, sourceID string, name string) (*api.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *slowSessionManager) Resume(ctx context.Context, id string) (*api.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *slowSessionManager) List(ctx context.Context, path string) ([]api.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *slowSessionManager) ListAll(ctx context.Context, limit int) ([]api.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type mockStore struct{}

func (m *mockStore) CreateSession(ctx context.Context, path string) (*api.Session, error) {
	return nil, nil
}
func (m *mockStore) GetSession(ctx context.Context, id string) (*api.Session, error) { return nil, nil }
func (m *mockStore) GetLastSession(ctx context.Context, path string) (*api.Session, error) {
	return nil, nil
}
func (m *mockStore) ListSessions(ctx context.Context, path string, limit int) ([]api.Session, error) {
	return nil, nil
}
func (m *mockStore) ListAllSessions(ctx context.Context, limit int) ([]api.Session, error) {
	return nil, nil
}
func (m *mockStore) UpdateSession(ctx context.Context, session *api.Session) error { return nil }
func (m *mockStore) DeleteSession(ctx context.Context, id string) error            { return nil }
func (m *mockStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	return nil
}
func (m *mockStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	return nil, nil
}
func (m *mockStore) ClearMessages(ctx context.Context, sessionID string) error { return nil }
func (m *mockStore) ReplaceMessages(ctx context.Context, sessionID string, msgs []api.Message) error {
	return nil
}
func (m *mockStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error { return nil }
func (m *mockStore) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	return nil, nil
}
func (m *mockStore) CountTurns(ctx context.Context, sessionID string, state api.TurnState) (int, error) {
	return 0, nil
}
func (m *mockStore) Close() error { return nil }

type fakeGitProvider struct {
	commitCalled bool
	commitMsg    string
	diffOutput   string
	diffErr      error
	err          error
}

func (f *fakeGitProvider) Status(ctx context.Context) (string, error) { return "", nil }
func (f *fakeGitProvider) Diff(ctx context.Context, path string) (string, error) {
	return f.diffOutput, f.diffErr
}
func (f *fakeGitProvider) Commit(ctx context.Context, message string) error {
	f.commitCalled = true
	f.commitMsg = message
	return f.err
}
func (f *fakeGitProvider) IsRepo(ctx context.Context) (bool, error)   { return true, nil }
func (f *fakeGitProvider) Branch(ctx context.Context) (string, error) { return "main", nil }

type fakeTurnManager struct {
	events []api.TurnEvent
}

func (f *fakeTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	ch := make(chan api.TurnEvent, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (f *fakeTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	return nil
}

// configured list of sessions for testing the sessions picker.
type fakeSessionManagerWithSessions struct {
	sessions []api.Session
	err      error
}

func (f *fakeSessionManagerWithSessions) CurrentSessionID() string { return "test" }
func (f *fakeSessionManagerWithSessions) ClearMessages(ctx context.Context, id string) error {
	return nil
}
func (f *fakeSessionManagerWithSessions) Rename(ctx context.Context, id string, name string) error {
	return nil
}
func (f *fakeSessionManagerWithSessions) Fork(ctx context.Context, sourceID string, name string) (*api.Session, error) {
	return nil, nil
}
func (f *fakeSessionManagerWithSessions) Resume(ctx context.Context, id string) (*api.Session, error) {
	return &api.Session{ID: id, Path: "/tmp"}, nil
}
func (f *fakeSessionManagerWithSessions) List(ctx context.Context, path string) ([]api.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []api.Session
	for _, s := range f.sessions {
		if s.Path == path {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeSessionManagerWithSessions) ListAll(ctx context.Context, limit int) ([]api.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions, nil
}

func TestCheckpointMsg_Success(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	gp := &fakeGitProvider{}
	m.SetGitProvider(gp)

	updated, cmd := m.Update(CheckpointMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for checkpoint")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if !gp.commitCalled {
		t.Error("expected Commit to be called on git provider")
	}
	if gp.commitMsg != "" {
		t.Errorf("commit message = %q, want empty (default)", gp.commitMsg)
	}
	if model2.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model2.state)
	}
	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "Checkpoint created") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected success message, got messages: %v", model2.messages)
	}
}

func TestCheckpointMsg_Error(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	gp := &fakeGitProvider{err: errors.New("git failed")}
	m.SetGitProvider(gp)

	updated, cmd := m.Update(CheckpointMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for checkpoint")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if !gp.commitCalled {
		t.Error("expected Commit to be called on git provider")
	}
	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "checkpoint failed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error message, got messages: %v", model2.messages)
	}
}

func TestCheckpointMsg_NoGitProvider(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(CheckpointMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for checkpoint")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "no git provider available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected no-provider message, got messages: %v", model2.messages)
	}
}

func TestSessionsMsg_WithSessions(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	now := time.Now()
	sm := &fakeSessionManagerWithSessions{
		sessions: []api.Session{
			{ID: "s1", Path: "/tmp", UpdatedAt: now},
			{ID: "s2", Path: "/tmp", UpdatedAt: now.Add(-time.Hour)},
		},
	}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(SessionsMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for sessions")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.sessionPicker == nil {
		t.Fatal("expected session picker to open")
	}
	if model2.sessionPicker.AllMode() {
		t.Error("expected picker to start in current-directory mode")
	}
	if len(model2.sessionPicker.Selected().ID) == 0 && model2.sessionPicker.HasSelection() {
		t.Error("expected first session to be selected")
	}
}

func TestSessionsMsg_Error(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	sm := &fakeSessionManagerWithSessions{err: errors.New("db error")}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(SessionsMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for sessions")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "list sessions") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error message, got messages: %v", model2.messages)
	}
}

func TestSessionsMsg_Empty(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	sm := &fakeSessionManagerWithSessions{sessions: []api.Session{}}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(SessionsMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for sessions")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "No sessions found") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected no-sessions message, got messages: %v", model2.messages)
	}
}

func TestSessionsMsg_NoSessionManager(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(SessionsMsg{})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for sessions")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "session manager not available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected no-session-manager message, got messages: %v", model2.messages)
	}
}

func TestCompactTimeout(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	appCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m.appCtx = appCtx

	m.SetCompressor(&slowCompressor{})
	m.SetStore(&mockStore{})

	updated, cmd := m.Update(input.SendMsg{Content: "/compact"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg, got %T", msg)
	}
	if !errors.Is(errMsg.Err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got %v", errMsg.Err)
	}
	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
}

func TestClearMessagesTimeout(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	appCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m.appCtx = appCtx

	m.SetSessionManager(&slowSessionManager{})

	updated, cmd := m.Update(input.SendMsg{Content: "/clear"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg, got %T", msg)
	}
	if !errors.Is(errMsg.Err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got %v", errMsg.Err)
	}
	if len(model.messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(model.messages))
	}
}

func TestWindowSizeMsg_DebouncesRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	countBefore := m.rebuildCount

	// Fire several WindowSizeMsg events in rapid succession.
	sizes := []tea.WindowSizeMsg{
		{Width: 121, Height: 41},
		{Width: 122, Height: 42},
		{Width: 122, Height: 42},
		{Width: 123, Height: 43},
	}
	var allCmds []tea.Cmd
	for _, sz := range sizes {
		_, cmds := m.Update(sz)
		allCmds = append(allCmds, cmds)
	}

	// There should be exactly one debounced tick scheduled.
	tickCount := 0
	var tickMsg debouncedResizeMsg
	for _, cmd := range allCmds {
		if cmd == nil {
			continue
		}
		msg := cmd()
		if drm, ok := msg.(debouncedResizeMsg); ok {
			tickCount++
			tickMsg = drm
		}
	}
	if tickCount != 1 {
		t.Fatalf("expected 1 debounced resize tick, got %d", tickCount)
	}

	// The expensive rebuild must not have happened yet.
	if m.rebuildCount != countBefore {
		t.Fatalf("rebuild happened before debounce: %d vs %d", m.rebuildCount, countBefore)
	}

	// Execute the tick to trigger the deferred rebuild.
	m.Update(tickMsg)
	if m.rebuildCount != countBefore+1 {
		t.Fatalf("expected exactly one rebuild after debounce, got %d", m.rebuildCount-countBefore)
	}
}

func TestWindowSizeMsg_UnchangedDimensions_NoRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	countBefore := m.rebuildCount

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if m.rebuildCount != countBefore {
		t.Errorf("rebuild count changed for unchanged dimensions")
	}
	if m.pendingResize {
		t.Error("pendingResize should be false for unchanged dimensions")
	}
}

func TestDiffCommand_RendersGitDiffOutput(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.SetGitProvider(&fakeGitProvider{diffOutput: "+added line"})

	updated, cmd := m.Update(input.SendMsg{Content: "/diff file.go"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /diff")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "added line") {
		t.Errorf("viewport should contain diff output, got %q", view)
	}
}

func TestDiffCommand_NoGitProviderShowsError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/diff file.go"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /diff")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "no git provider") {
		t.Errorf("viewport should show no-git-provider error, got %q", view)
	}
}

func TestDiffCommand_EmptyDiffShowsError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.SetGitProvider(&fakeGitProvider{diffOutput: ""})

	updated, cmd := m.Update(input.SendMsg{Content: "/diff file.go"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /diff")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "no diff") {
		t.Errorf("viewport should show no-diff message, got %q", view)
	}
}

func TestStreamChunk_IncrementalRenderMatchesRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Add static messages to form a stable prefix.
	m.addMessage(msgcomp.NewUserMessage("first static message", m.styles))
	m.addMessage(msgcomp.NewUserMessage("second static message", m.styles))
	m.rb.markClean()

	// Stream several chunks into a new assistant message.
	chunks := []string{"one ", "two ", "three"}
	for _, c := range chunks {
		m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: c}})
	}

	got := m.rb.String()
	m.rebuildRenderedContent()
	want := m.rb.String()

	if got != want {
		t.Errorf("incremental render does not match rebuild:\ngot:\n%s\n\nwant:\n%s", got, want)
	}
}

type fakeMCPClient struct {
	tools []api.ToolDefinition
	err   error
}

func (f *fakeMCPClient) Connect(ctx context.Context) error { return nil }
func (f *fakeMCPClient) Close() error                      { return nil }
func (f *fakeMCPClient) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	return f.tools, f.err
}
func (f *fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return "", nil
}

func TestMCPCommand_ListsTools(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.SetMCPClient(&fakeMCPClient{tools: []api.ToolDefinition{
		{Name: "tool-a", Description: "first tool"},
		{Name: "tool-b", Description: "second tool"},
	}})

	updated, cmd := m.Update(input.SendMsg{Content: "/mcp"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /mcp")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "tool-a") {
		t.Errorf("viewport should contain tool-a, got %q", view)
	}
	if !strings.Contains(view, "tool-b") {
		t.Errorf("viewport should contain tool-b, got %q", view)
	}
}

func TestMCPCommand_NilClientShowsDisconnected(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/mcp"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /mcp")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "No MCP tools connected") {
		t.Errorf("viewport should show disconnected message, got %q", view)
	}
}

// TestGoldenViewIdle is a smoke golden test for the deterministic TUI harness.
func TestGoldenViewIdle(t *testing.T) {
	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, err := New(cfg, session, context.Background())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	m.width = 80
	m.height = 24
	m.updateLayout()

	compareGolden(t, "view_idle", m.View().Content)
}

func newGoldenModel(t *testing.T) *Model {
	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, err := New(cfg, session, context.Background())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.width = 80
	m.height = 24
	m.updateLayout()
	return m
}

func TestGoldenViewThinking(t *testing.T) {
	m := newGoldenModel(t)
	updated, _ := m.Update(StateChangeMsg{State: api.TurnThinking})
	m = updated.(*Model)
	compareGolden(t, "view_thinking", m.View().Content)
}

func TestGoldenViewToolCalls(t *testing.T) {
	m := newGoldenModel(t)
	m.SetToolCount(3)
	updated, _ := m.Update(StateChangeMsg{State: api.TurnToolCalls})
	m = updated.(*Model)
	compareGolden(t, "view_tool_calls", m.View().Content)
}

func TestGoldenViewError(t *testing.T) {
	m := newGoldenModel(t)
	updated, _ := m.Update(ErrorMsg{Err: errors.New("simulated failure")})
	m = updated.(*Model)
	compareGolden(t, "view_error", m.View().Content)
}

func TestGoldenViewWaitingApproval(t *testing.T) {
	m := newGoldenModel(t)
	updated, _ := m.Update(ApprovalRequestMsg{
		Calls: []api.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"foo.txt"}`},
		},
		RequestID: 1,
	})
	m = updated.(*Model)
	compareGolden(t, "view_waiting_approval", m.View().Content)
}

// capturingErrorTurnManager records the context passed to RunTurn and returns an error.
type capturingErrorTurnManager struct {
	capturedCtx context.Context
}

func (c *capturingErrorTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	c.capturedCtx = ctx
	return nil, errors.New("run turn failed")
}

func (c *capturingErrorTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	return nil
}

func TestRunTurnError_CancelsContext(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	tm := &capturingErrorTurnManager{}
	m.SetTurnManager(tm)

	m.Update(input.SendMsg{Content: "hello"})

	if tm.capturedCtx == nil {
		t.Fatal("RunTurn was not called")
	}
	if !errors.Is(tm.capturedCtx.Err(), context.Canceled) {
		t.Errorf("expected canceled context after RunTurn error, got %v", tm.capturedCtx.Err())
	}
}

// errorResumeTurnManager returns an error from ResumeWithApproval.
type errorResumeTurnManager struct {
	resumeErr error
}

func (e *errorResumeTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (e *errorResumeTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	return e.resumeErr
}

func TestApprovalResponse_ResumeWithApprovalErrorReturnsErrorMsg(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	tm := &errorResumeTurnManager{resumeErr: errors.New("resume failed")}
	m.SetTurnManager(tm)

	calls := []api.ToolCall{{ID: "1", Name: "edit_file", Arguments: `{}`}}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 7})

	updated, cmd := m.Update(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "1"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command after approval response")
	}

	// The response produces a batch with the resume command and a stream poll.
	var errMsg ErrorMsg
	batch := cmd()
	if bm, ok := batch.(tea.BatchMsg); ok {
		found := false
		for _, c := range bm {
			if c == nil {
				continue
			}
			if msg, ok := c().(ErrorMsg); ok {
				errMsg = msg
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected ErrorMsg in batch, got %T", batch)
		}
	} else if em, ok := batch.(ErrorMsg); ok {
		errMsg = em
	} else {
		t.Fatalf("expected ErrorMsg, got %T", batch)
	}

	if !strings.Contains(errMsg.Err.Error(), "resume failed") {
		t.Errorf("expected error to contain 'resume failed', got %v", errMsg.Err)
	}

	updated2, _ := model.Update(errMsg)
	model2 := updated2.(*Model)
	if model2.state != api.TurnError {
		t.Errorf("state = %d, want TurnError", model2.state)
	}
}

func TestReadStreamChunk_IgnoresStaleEventsAfterCancel(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "stale"}

	m.mu.Lock()
	m.streamCh = ch
	m.streamCanceled = true
	m.mu.Unlock()

	cmd := m.readStreamChunk()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if msg != nil {
		t.Errorf("expected stale event to be ignored, got %T", msg)
	}
}

func TestRenderApprovalDialog_NilSession(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: t.TempDir()}
	m, _ := New(cfg, session, context.Background())
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{{ID: "1", Name: "write_file", Arguments: `{}`}}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})

	// Simulate a session being cleared while the dialog is active.
	m.session = nil

	background := "background"
	view := m.renderApprovalDialog(background)
	if view != background {
		t.Errorf("expected background unchanged when session is nil, got %q", view)
	}
}

func TestApprovalResponse_DedupesAlwaysAllNames(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	var setNames []string
	m.SetAutoApproveSetter(func(name string) {
		setNames = append(setNames, name)
	})

	calls := []api.ToolCall{
		{ID: "1", Name: "write_file", Arguments: `{}`},
		{ID: "2", Name: "write_file", Arguments: `{}`},
	}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})

	m.Update(ApprovalResponseMsg{Decision: api.ApprovalAlways, CallID: "1"})

	if len(setNames) != 1 {
		t.Errorf("autoApproveSetter called %d times, want 1", len(setNames))
	}
	if len(setNames) > 0 && setNames[0] != "write_file" {
		t.Errorf("autoApproveSetter name = %q, want write_file", setNames[0])
	}
}

func TestHelpCommand_OpensOverlay(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/help"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for /help")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if !model2.showHelp {
		t.Fatal("expected help overlay to be open")
	}

	view := model2.View().Content
	if !strings.Contains(view, "Keyboard shortcuts") {
		t.Errorf("view should contain help title, got %q", view)
	}
	if !strings.Contains(view, "/help") {
		t.Errorf("view should contain /help command, got %q", view)
	}
}

func TestHelpOverlay_CloseKeys(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(ShowHelpMsg{})
	m = updated.(*Model)
	if !m.showHelp {
		t.Fatal("expected help overlay to be open")
	}

	tests := []rune{
		tea.KeyEsc,
		tea.KeyEnter,
		'q',
	}

	for _, code := range tests {
		updated, _ = m.Update(tea.KeyPressMsg{Code: code})
		model := updated.(*Model)
		if model.showHelp {
			t.Errorf("help overlay should close on %q", code)
		}
		// reopen for next key
		m = model
		updated, _ = m.Update(ShowHelpMsg{})
		m = updated.(*Model)
	}
}

func TestHelpOverlay_ScrollKeys(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(ShowHelpMsg{})
	if !m.showHelp {
		t.Fatal("expected help overlay to be open")
	}

	if m.helpPanel.Offset() != 0 {
		t.Fatalf("initial offset = %d, want 0", m.helpPanel.Offset())
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.helpPanel.Offset() == 0 {
		t.Error("help overlay should scroll down")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.helpPanel.Offset() != 0 {
		t.Errorf("help overlay should clamp to top after pgup, got offset %d", m.helpPanel.Offset())
	}
}
