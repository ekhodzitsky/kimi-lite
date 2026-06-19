package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestCycleFocus_Forward(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Default focus is input.
	if m.focused != focusInput {
		t.Fatalf("initial focus = %d, want focusInput", m.focused)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := updated.(*Model)
	if model.focused != focusViewport {
		t.Errorf("first tab focus = %d, want focusViewport", model.focused)
	}

	updated2, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model2 := updated2.(*Model)
	if model2.focused != focusInput {
		t.Errorf("second tab focus = %d, want focusInput", model2.focused)
	}
}

func TestCycleFocus_Backward(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.focused = focusViewport

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model := updated.(*Model)
	if model.focused != focusInput {
		t.Errorf("shift+tab from viewport = %d, want focusInput", model.focused)
	}

	// Shift+Tab from input toggles plan mode instead of cycling focus.
	updated2, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model2 := updated2.(*Model)
	if !model2.input.PlanMode() {
		t.Error("shift+tab from input should enable plan mode")
	}
}

func TestHandleKeyMsg_ApprovalKeys(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{{ID: "1", Name: "write_file", Arguments: `{}`}}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})

	tests := []struct {
		key      rune
		tool     string
		decision api.ApprovalDecision
	}{
		{'y', "read_file", api.ApprovalYes},
		{'n', "read_file", api.ApprovalNo},
		{'a', "read_file", api.ApprovalAlways},
		{'d', "write_file", api.ApprovalDiff},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			m2, _ := New(cfg, session, context.Background(), "")
			m2.width = 120
			m2.height = 40
			m2.updateLayout()
			m2.Update(ApprovalRequestMsg{Calls: []api.ToolCall{{ID: "1", Name: tt.tool, Arguments: `{}`}}, RequestID: 1})

			updated, cmd := m2.Update(tea.KeyPressMsg{Code: tt.key, Text: string(tt.key)})
			model := updated.(*Model)
			if cmd == nil {
				t.Fatal("expected command from approval key")
			}
			resp, ok := findApprovalResponse(cmd)
			if !ok {
				t.Fatalf("expected ApprovalResponseMsg in command output, got %T", cmd())
			}
			if resp.Decision != tt.decision {
				t.Errorf("decision = %v, want %v", resp.Decision, tt.decision)
			}
			if resp.CallID != "1" {
				t.Errorf("callID = %q, want 1", resp.CallID)
			}
			_ = model
		})
	}
}

// findApprovalResponse executes cmd and extracts an ApprovalResponseMsg from
// either a single message or a tea.BatchMsg.
func findApprovalResponse(cmd tea.Cmd) (ApprovalResponseMsg, bool) {
	if cmd == nil {
		return ApprovalResponseMsg{}, false
	}
	msg := cmd()
	if resp, ok := msg.(ApprovalResponseMsg); ok {
		return resp, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return ApprovalResponseMsg{}, false
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if resp, ok := c().(ApprovalResponseMsg); ok {
			return resp, true
		}
	}
	return ApprovalResponseMsg{}, false
}

func TestHandleKeyMsg_ApprovalKeyNoActiveCall(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Force waiting-approval state without starting a request.
	m.setState(api.TurnWaitingApproval)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model := updated.(*Model)
	if _, ok := findApprovalResponse(cmd); ok {
		t.Error("expected no ApprovalResponseMsg when approval controller has no active call")
	}
	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}
}

func TestHandleKeyMsg_UnknownKey(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	stateBefore := m.state
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	model := updated.(*Model)
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Error("unknown key should not produce quit command")
		}
	}
	if model.state != stateBefore {
		t.Errorf("state changed unexpectedly from %d to %d", stateBefore, model.state)
	}
}

func TestHandleMouseMsg_RightButtonIgnored(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.focused = focusInput
	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseRight, X: 5, Y: 5})
	model := updated.(*Model)
	if model.focused != focusInput {
		t.Errorf("focus = %d, want focusInput after right click", model.focused)
	}
}

func TestHandleMouseMsg_ViewportClick(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Add a message to hide the welcome panel so the viewport occupies its
	// usual full-height region.
	m.addMessage(msgcomp.NewUserMessage("hello", m.styles))
	m.updateLayout()

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 5, Y: 5})
	model := updated.(*Model)
	if model.focused != focusViewport {
		t.Errorf("focus = %d, want focusViewport after viewport click", model.focused)
	}
}

func TestHandleMouseMsg_InputClick(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Add a message to hide the welcome panel so the viewport/input geometry
	// matches the assumptions of this test.
	m.addMessage(msgcomp.NewUserMessage("hello", m.styles))
	m.updateLayout()

	l := m.layout()
	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 50, Y: l.vpHeight})
	model := updated.(*Model)
	if model.focused != focusInput {
		t.Errorf("focus = %d, want focusInput after input click", model.focused)
	}
}

func TestUpdateLayout_EqualLayout_NoRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	countBefore := m.rebuildCount
	m.updateLayout()
	if m.rebuildCount != countBefore {
		t.Errorf("rebuild count changed for equal layout: %d vs %d", m.rebuildCount, countBefore)
	}
}

func TestReadStreamChunk_NilChannel(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.streamCh = nil

	cmd := m.readStreamChunk()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	chunk, ok := msg.(StreamChunkMsg)
	if !ok {
		t.Fatalf("expected StreamChunkMsg, got %T", msg)
	}
	if !chunk.Chunk.Done {
		t.Error("expected done chunk for nil channel")
	}
}

func TestReadStreamChunk_UnknownEventType(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventType(255)}
	close(ch)
	m.streamCh = ch

	cmd := m.readStreamChunk()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil for unknown event type, got %T", msg)
	}
}

func TestReadStreamChunk_ApprovalDiffEvent(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventApprovalDiff, DiffCallID: "call-1", DiffContent: "diff"}
	m.streamCh = ch

	cmd := m.readStreamChunk()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	diff, ok := msg.(ApprovalDiffMsg)
	if !ok {
		t.Fatalf("expected ApprovalDiffMsg, got %T", msg)
	}
	if diff.CallID != "call-1" {
		t.Errorf("callID = %q, want call-1", diff.CallID)
	}
	if diff.Diff != "diff" {
		t.Errorf("diff = %q, want diff", diff.Diff)
	}
}

func TestReadStreamChunk_ToolResultEvent(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventToolResult, Result: api.ToolResult{CallID: "call-1", Output: "out"}}
	m.streamCh = ch

	cmd := m.readStreamChunk()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	result, ok := msg.(ToolResultMsg)
	if !ok {
		t.Fatalf("expected ToolResultMsg, got %T", msg)
	}
	if result.Result.CallID != "call-1" {
		t.Errorf("callID = %q, want call-1", result.Result.CallID)
	}
}

func TestHandleStreamChunk_LastBlockStartZeroAfterRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(input.SendMsg{Content: "hello"})
	m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "world"}})

	// Simulate a full rebuild while streaming (e.g. after a resize debounce).
	m.rebuildRenderedContent()
	if m.rb.lastBlockStart() != 0 {
		t.Fatalf("lastBlockStart should be 0 after rebuild, got %d", m.rb.lastBlockStart())
	}

	updated, _ := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "!"}})
	model := updated.(*Model)
	if model.state != api.TurnStreaming {
		t.Errorf("state = %d, want TurnStreaming", model.state)
	}
	view := model.vp.View().Content
	if !strings.Contains(view, "world!") {
		t.Errorf("viewport should contain streamed content, got %q", view)
	}
}

func TestHandleStreamChunk_FallbackRebuild(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	tm := &fakeTurnManager{
		events: []api.TurnEvent{
			{Type: api.TurnEventContent, Content: "thinking"},
			{Type: api.TurnEventDone, ToolCalls: []api.ToolCall{
				{ID: "call-1", Name: "read_file", Arguments: `{}`},
			}},
			{Type: api.TurnEventToolResult, Result: api.ToolResult{CallID: "call-1", Output: "done"}},
			{Type: api.TurnEventContent, Content: " done"},
			{Type: api.TurnEventDone},
		},
	}
	m.SetTurnManager(tm)

	updated, cmd := m.Update(input.SendMsg{Content: "read file"})
	model := updated.(*Model)

	// Drain all commands.
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		updated, cmd = model.Update(msg)
		model = updated.(*Model)
	}

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	view := model.vp.View().Content
	if !strings.Contains(view, "thinking") {
		t.Errorf("viewport should contain initial assistant content, got %q", view)
	}
	if !strings.Contains(view, "done") {
		t.Errorf("viewport should contain tool result content, got %q", view)
	}
}

func TestRenderBuffer_AppendBlock_WithActiveContent(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	rb.rebuild([]string{"prefix"})
	rb.setLastBlockStart(len("prefix"))
	rb.updateLastBlock("active")

	rb.appendBlock("newblock")

	want := "prefix\n\nactive\n\nnewblock"
	if got := rb.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestRenderBuffer_Len_WithPrefixAndCompleted(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	rb.rebuild([]string{"a", "b"})
	want := len("a") + len("\n\n") + len("b")
	if got := rb.len(); got != want {
		t.Errorf("len() = %d, want %d", got, want)
	}

	// Append block with prefix that already ends with separator.
	rb.reset()
	rb.prefix = "x\n\n"
	rb.prefixHasSep = true
	rb.completed = []string{"y"}
	want = len("x\n\n") + len("y")
	if got := rb.len(); got != want {
		t.Errorf("len() with prefixHasSep = %d, want %d", got, want)
	}
}

func TestRenderApprovalDialog_NonEditTool(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"foo.txt"}`}}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})

	view := m.renderApprovalDialog("background")
	if !strings.Contains(view, "foo.txt") {
		t.Errorf("dialog should contain arguments for non-edit tool, got %q", view)
	}
}

func TestHandleCommand_MCPToolsError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.SetMCPClient(&fakeMCPClient{err: errors.New("list failed")})

	updated, cmd := m.Update(input.SendMsg{Content: "/mcp"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command for /mcp")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "list failed") {
		t.Errorf("viewport should show mcp error, got %q", view)
	}
}

func TestHandleCommand_TitleNoSessionManager(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetSessionManager(nil)

	updated, cmd := m.Update(input.SendMsg{Content: "/title New Name"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "no session to rename") {
		t.Errorf("viewport should show no-session error, got %q", view)
	}
}

func TestHandleCommand_ClearDuringActiveTurn(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.setState(api.TurnStreaming)

	updated, cmd := m.Update(input.SendMsg{Content: "/clear"})
	model := updated.(*Model)
	if cmd != nil {
		t.Error("expected no command for /clear during active turn")
	}
	if len(model.messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(model.messages))
	}
	if model.state != api.TurnStreaming {
		t.Errorf("state = %d, want TurnStreaming", model.state)
	}
}

func TestHandleCommand_CompactContextMaxAdjustsKeepRecent(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Behavior.CompactKeepRecent = 2
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetContextStats(0, 256000) // contextMax > 0

	comp := &recordingCompressor{}
	m.SetCompressor(comp)
	m.SetStore(&mockStore{})

	updated, cmd := m.Update(input.SendMsg{Content: "/compact"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command")
	}

	msg := cmd()
	if _, ok := msg.(CompactResultMsg); !ok {
		t.Fatalf("expected CompactResultMsg, got %T", msg)
	}
	want := min(2, 256000/64000)
	if want < 2 {
		want = 2
	}
	// contextMax/64000 = 4, max(2, 4) = 4.
	if comp.gotKeepRecent != 4 {
		t.Errorf("keepRecent = %d, want 4", comp.gotKeepRecent)
	}
	_ = model
}

func TestApprovalController_HandleResponse_LateAlways(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)
	ac.index = 2 // past the end

	done, approvals, alwaysAll := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalAlways, CallID: "a"})
	if !done {
		t.Error("expected done for late ApprovalAlways")
	}
	if !alwaysAll {
		t.Error("expected alwaysAll=true")
	}
	if approvals["a"] != api.ApprovalYes || approvals["b"] != api.ApprovalYes {
		t.Errorf("expected all approvals yes, got %v", approvals)
	}
}

func TestApprovalController_HandleResponse_LateYesForKnownCall(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)
	ac.index = 2

	done, approvals, alwaysAll := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "b"})
	if !done {
		t.Error("expected done for late yes on known call")
	}
	if alwaysAll {
		t.Error("expected alwaysAll=false")
	}
	if approvals["a"] != api.ApprovalNo {
		t.Errorf("approvals[a] = %v, want ApprovalNo", approvals["a"])
	}
	if approvals["b"] != api.ApprovalYes {
		t.Errorf("approvals[b] = %v, want ApprovalYes", approvals["b"])
	}
}

func TestApprovalController_IsKnownCall(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{{ID: "a", Name: "read_file"}}, 1)

	if !ac.isKnownCall("a") {
		t.Error("expected a to be known")
	}
	if ac.isKnownCall("z") {
		t.Error("expected z to be unknown")
	}
}

func TestHandleApprovalResponse_NotDone(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "write_file", Arguments: `{}`},
		{ID: "2", Name: "write_file", Arguments: `{}`},
	}
	m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})

	updated, _ := m.Update(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "1"})
	model := updated.(*Model)

	if model.state != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", model.state)
	}
	if model.approval.currentIndex() != 1 {
		t.Errorf("currentIndex = %d, want 1", model.approval.currentIndex())
	}
}

func TestCompactResultMsg_ZeroCount(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(CompactResultMsg{Count: 0})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "Nothing to compact") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected nothing-to-compact message, got %v", model.messages)
	}
}

func TestMCPListMsg_EmptyTools(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(MCPListMsg{Tools: []api.ToolDefinition{}})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "No MCP tools available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected no-tools message, got %v", model.messages)
	}
}

func TestSetApprovalModeSetter(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")

	var called bool
	m.SetApprovalModeSetter(func(int) { called = true })
	if m.approvalModeSetter == nil {
		t.Fatal("approvalModeSetter should be set")
	}
	m.approvalModeSetter(0)
	if !called {
		t.Error("expected setter to be called")
	}
}

func TestAddMessage_SetsWidth(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	msg := msgcomp.NewUserMessage("hello", m.styles)
	m.addMessage(msg)
	if msg.Width != m.vpWidth() {
		t.Errorf("message width = %d, want %d", msg.Width, m.vpWidth())
	}
}

func TestHandleSend_BlockedDuringActiveTurn(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.setState(api.TurnThinking)

	updated, cmd := m.Update(input.SendMsg{Content: "second message"})
	model := updated.(*Model)
	if cmd != nil {
		t.Error("expected no command when sending during active turn")
	}
	if len(model.messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(model.messages))
	}
}

func TestHandleSend_WithTurnManagerError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Make RunTurn return an error.
	m.SetTurnManager(&errorRunTurnManager{err: errors.New("run turn failed")})

	updated, cmd := m.Update(input.SendMsg{Content: "hello"})
	model := updated.(*Model)
	if model.state != api.TurnThinking {
		t.Errorf("state = %d, want TurnThinking while async call is in flight", model.state)
	}
	if cmd == nil {
		t.Fatal("expected async command after send")
	}

	msg := cmd()
	runResult, ok := msg.(RunTurnResultMsg)
	if !ok {
		t.Fatalf("expected RunTurnResultMsg, got %T", msg)
	}

	updated2, _ := model.Update(runResult)
	model2 := updated2.(*Model)
	if model2.state != api.TurnError {
		t.Errorf("state = %d, want TurnError after RunTurn error", model2.state)
	}
}

// errorRunTurnManager always returns an error from RunTurn.
type errorRunTurnManager struct {
	err error
}

func (e *errorRunTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, e.err
}

func (e *errorRunTurnManager) RunTurnWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error) {
	return e.RunTurn(ctx, sessionID, input)
}

func (e *errorRunTurnManager) RunTurnWithPlan(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, e.err
}

func (e *errorRunTurnManager) RunTurnWithPlanWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error) {
	return e.RunTurnWithPlan(ctx, sessionID, input)
}

func (e *errorRunTurnManager) ResumeWithPlan(ctx context.Context, sessionID string, approved bool) error {
	return nil
}

func (e *errorRunTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	return nil
}

func (e *errorRunTurnManager) Steer(ctx context.Context, sessionID string, input string) error {
	return nil
}

func TestViewportFocusDoesNotSendKeysToInput(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.input.SetValue("typed")
	m.focused = focusViewport

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model := updated.(*Model)

	if model.input.Value() != "typed" {
		t.Errorf("input value changed to %q while viewport is focused", model.input.Value())
	}
}

func TestTabNavigatesCompletionWhenPopupOpen(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.input.SetFileCandidates([]string{"a.go", "b.go", "c.go"})

	// Type "@" to open the mention completion popup.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	m = updated.(*Model)
	if !m.input.Completing() {
		t.Fatal("expected completion popup to be open after typing @")
	}

	// Tab should navigate the popup, not cycle focus.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := updated.(*Model)

	if !model.input.Completing() {
		t.Error("Tab should navigate completion, not change focus")
	}
	if model.focused != focusInput {
		t.Errorf("focus should remain on input, got %d", model.focused)
	}
}
