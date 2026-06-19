package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestHelpData_UsesConfiguredKeybindings(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Keybindings.Send = "ctrl+enter"
	cfg.Keybindings.Newline = "enter"
	cfg.Keybindings.Cancel = "ctrl+["
	cfg.Keybindings.Quit = "ctrl+q"
	cfg.Keybindings.Yolo = "ctrl+o"
	cfg.Keybindings.FocusNext = "ctrl+n"
	cfg.Keybindings.FocusPrev = "ctrl+p"
	cfg.Keybindings.ApproveYes = "f1"
	cfg.Keybindings.ApproveNo = "f2"
	cfg.Keybindings.ApproveAlways = "f3"
	cfg.Keybindings.ApproveDiff = "f4"
	cfg.Keybindings.ExternalEditor = "ctrl+x"
	cfg.Keybindings.Steer = "ctrl+t"
	cfg.Keybindings.Paste = "ctrl+shift+v"

	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")

	data := m.helpData()
	want := map[string]bool{
		"ctrl+enter (input)": false,
		"enter":              false,
		"ctrl+[":             false,
		"ctrl+q":             false,
		"ctrl+o":             false,
		"ctrl+n":             false,
		"ctrl+p":             false,
		"ctrl+x":             false,
		"ctrl+t":             false,
		"ctrl+shift+v":       false,
	}
	for _, s := range data.Shortcuts {
		if _, ok := want[s.Keys]; ok {
			want[s.Keys] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("expected help data to contain shortcut %q", k)
		}
	}
}

func TestHelpData_FallsBackToDefaults(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Keybindings.Send = ""
	cfg.Keybindings.Newline = ""
	cfg.Keybindings.Cancel = ""
	cfg.Keybindings.Quit = ""
	cfg.Keybindings.Yolo = ""
	cfg.Keybindings.FocusNext = ""
	cfg.Keybindings.FocusPrev = ""
	cfg.Keybindings.ApproveYes = ""
	cfg.Keybindings.ApproveNo = ""
	cfg.Keybindings.ApproveAlways = ""
	cfg.Keybindings.ApproveDiff = ""
	cfg.Keybindings.ExternalEditor = ""
	cfg.Keybindings.Steer = ""
	cfg.Keybindings.Paste = ""

	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.state = api.TurnWaitingApproval

	data := m.helpData()
	want := map[string]bool{
		"enter (input)":               false,
		"alt+enter":                   false,
		"esc":                         false,
		"ctrl+c":                      false,
		"ctrl+y":                      false,
		"tab":                         false,
		"shift+tab":                   false,
		"ctrl+g":                      false,
		"ctrl+s":                      false,
		"ctrl+v":                      false,
		"y/n/a/d":                     false,
		"enter (viewport, tool call)": false,
	}
	for _, s := range data.Shortcuts {
		switch s.Keys {
		case "enter (input)", "alt+enter", "esc", "ctrl+c", "ctrl+y", "tab", "shift+tab", "ctrl+g", "ctrl+s", "ctrl+v", "y/n/a/d", "enter (viewport, tool call)":
			want[s.Keys] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("expected help data to contain shortcut %q", k)
		}
	}
}

func TestHelpData_ContextSensitive(t *testing.T) {
	t.Parallel()

	session := &api.Session{ID: "test", Path: "/tmp"}

	t.Run("approval waiting", func(t *testing.T) {
		t.Parallel()
		cfg := config.DefaultConfig()
		cfg.Keybindings.ApproveYes = "f1"
		cfg.Keybindings.ApproveNo = "f2"
		cfg.Keybindings.ApproveAlways = "f3"
		cfg.Keybindings.ApproveDiff = "f4"
		m, _ := New(cfg, session, context.Background(), "")
		m.state = api.TurnWaitingApproval

		data := m.helpData()
		found := false
		for _, s := range data.Shortcuts {
			if s.Keys == "f1/f2/f3/f4" && s.Description == "Approve yes/no/always/diff" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected approval shortcut, got %+v", data.Shortcuts)
		}
	})

	t.Run("plan pending", func(t *testing.T) {
		t.Parallel()
		cfg := config.DefaultConfig()
		m, _ := New(cfg, session, context.Background(), "")
		m.planPending = true

		data := m.helpData()
		found := false
		for _, s := range data.Shortcuts {
			if s.Keys == "enter/y" && s.Description == "Approve plan" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected plan approve shortcut, got %+v", data.Shortcuts)
		}
	})

	t.Run("steer open", func(t *testing.T) {
		t.Parallel()
		cfg := config.DefaultConfig()
		cfg.Keybindings.Send = "f5"
		m, _ := New(cfg, session, context.Background(), "")
		m.steerOpen = true

		data := m.helpData()
		found := false
		for _, s := range data.Shortcuts {
			if s.Keys == "f5" && s.Description == "Send steering instruction" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected steer send shortcut, got %+v", data.Shortcuts)
		}
	})
}

func TestHelpKey_TogglesHelpWhenInputEmpty(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	if m.input.Value() != "" {
		t.Fatal("expected empty input at start")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	model := updated.(*Model)
	if !model.showHelp {
		t.Fatal("expected help overlay to open on ? with empty input")
	}
}

func TestHelpKey_DoesNotToggleWhenInputNotEmpty(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.input.SetValue("hello")

	updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	model := updated.(*Model)
	if model.showHelp {
		t.Error("expected help overlay to stay closed when input is not empty")
	}
	if !strings.Contains(model.input.Value(), "?") {
		t.Errorf("expected ? to be typed into input, got %q", model.input.Value())
	}
}

func TestHelpKey_TogglesHelpOffWhenOpen(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	updated, _ := m.Update(ShowHelpMsg{})
	model := updated.(*Model)
	if !model.showHelp {
		t.Fatal("expected help overlay to be open")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	model = updated.(*Model)
	if model.showHelp {
		t.Fatal("expected help overlay to close on ? when already open")
	}
}

func TestPlanApproval_RespectsConfiguredSendCancel(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Keybindings.Send = "f5"
	cfg.Keybindings.Cancel = "f6"
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.planPending = true

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF5, Text: "f5"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for configured Send key")
	}
	if msg, ok := cmd().(PlanApprovalMsg); !ok || !msg.Approved {
		t.Fatalf("expected approved PlanApprovalMsg, got %T %v", cmd(), msg)
	}

	m = model
	m.planPending = true
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyF6, Text: "f6"})
	model = updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for configured Cancel key")
	}
	if msg, ok := cmd().(PlanApprovalMsg); !ok || msg.Approved {
		t.Fatalf("expected rejected PlanApprovalMsg, got %T %v", cmd(), msg)
	}
}

func TestSteerSend_RespectsConfiguredSend(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Keybindings.Send = "f5"
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.setState(api.TurnStreaming)
	m.steerOpen = true
	m.steerInput = "keep it short"
	m.steerCursor = len([]rune(m.steerInput))

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF5, Text: "f5"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for configured Send key")
	}
	msg := cmd()
	if steer, ok := msg.(SteerMsg); !ok || steer.Content != "keep it short" {
		t.Fatalf("expected SteerMsg, got %T %v", msg, msg)
	}
	if model.steerOpen {
		t.Error("expected steer overlay to close after sending")
	}

	// With a custom Send binding, the default enter should be treated as text,
	// not as the send action.
	m2, _ := New(cfg, session, context.Background(), "")
	m2.width = 80
	m2.height = 24
	m2.updateLayout()
	m2.setState(api.TurnStreaming)
	m2.steerOpen = true
	m2.steerInput = "do not send"
	m2.steerCursor = len([]rune(m2.steerInput))

	updated, cmd = m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	model2 := updated.(*Model)
	if cmd != nil {
		t.Fatalf("expected no command for default enter with custom Send, got %T", cmd())
	}
	if !model2.steerOpen {
		t.Error("expected steer overlay to stay open")
	}
	if !strings.Contains(model2.steerInput, "enter") {
		t.Errorf("expected enter to be appended as text, got %q", model2.steerInput)
	}
}

func TestPlanPanel_ScrollAndEnterEsc(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 12
	m.updateLayout()

	var planLines []string
	for i := 1; i <= 20; i++ {
		planLines = append(planLines, strings.Repeat("x", 60))
	}
	plan := strings.Join(planLines, "\n")

	updated, _ := m.Update(PlanRequestMsg{Plan: plan})
	model := updated.(*Model)
	if !model.planPending {
		t.Fatal("expected plan panel open")
	}
	if model.planScrollOffset != 0 {
		t.Fatalf("initial scroll offset = %d, want 0", model.planScrollOffset)
	}

	maxHeight := model.planPanelMaxHeight()

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(*Model)
	if model.planScrollOffset != 1 {
		t.Errorf("after down offset = %d, want 1", model.planScrollOffset)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	model = updated.(*Model)
	if model.planScrollOffset != 1+maxHeight {
		t.Errorf("after pgdown offset = %d, want %d", model.planScrollOffset, 1+maxHeight)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(*Model)
	if model.planScrollOffset != maxHeight {
		t.Errorf("after up offset = %d, want %d", model.planScrollOffset, maxHeight)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	model = updated.(*Model)
	if model.planScrollOffset != 0 {
		t.Errorf("after pgup offset = %d, want 0", model.planScrollOffset)
	}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for Enter")
	}
	if _, ok := cmd().(PlanApprovalMsg); !ok {
		t.Fatalf("expected PlanApprovalMsg, got %T", cmd())
	}

	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command for Esc")
	}
	if msg, ok := cmd().(PlanApprovalMsg); !ok || msg.Approved {
		t.Fatalf("expected rejected PlanApprovalMsg, got %T", cmd())
	}
}

func TestPlanPanel_HeightCapped(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 12
	m.updateLayout()

	plan := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10"}, "\n")
	updated, _ := m.Update(PlanRequestMsg{Plan: plan})
	model := updated.(*Model)

	view := model.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Errorf("view height = %d, want %d", len(lines), model.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != model.width {
			t.Errorf("line %d width = %d, want %d", i, w, model.width)
		}
	}
	if !strings.Contains(view, "Plan requires approval") {
		t.Errorf("view should contain plan header, got:\n%s", view)
	}
}

func TestApprovalDialog_BatchProgress(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path":"a.txt"}`},
		{ID: "2", Name: "read_file", Arguments: `{"path":"b.txt"}`},
		{ID: "3", Name: "read_file", Arguments: `{"path":"c.txt"}`},
	}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	view := model.View().Content
	if !strings.Contains(view, "Call 1 of 3") {
		t.Errorf("expected 'Call 1 of 3' in view, got:\n%s", view)
	}

	updated, _ = model.Update(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "1"})
	model = updated.(*Model)
	view = model.View().Content
	if !strings.Contains(view, "Call 2 of 3") {
		t.Errorf("expected 'Call 2 of 3' after first approval, got:\n%s", view)
	}
}

func TestApprovalDialog_UnsafeToolHidesAlways(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{{ID: "1", Name: "write_file", Arguments: `{"path":"x.txt","content":"x"}`}}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	view := model.View().Content
	if strings.Contains(view, "Allow for this session") || strings.Contains(view, "always") {
		t.Errorf("expected always option hidden for write_file, got:\n%s", view)
	}
	if !strings.Contains(view, "1. Allow") || !strings.Contains(view, "2. Reject") || !strings.Contains(view, "4. Diff") {
		t.Errorf("expected Allow/Reject/Diff options, got:\n%s", view)
	}

	resp, ok := findApprovalResponse(tea.Batch(model.handleKeyMsg(tea.KeyPressMsg{Code: '3', Text: "3"})...))
	if ok {
		t.Errorf("expected no response for '3' on unsafe tool, got %v", resp)
	}

	resp, ok = findApprovalResponse(tea.Batch(model.handleKeyMsg(tea.KeyPressMsg{Code: 'a', Text: "a"})...))
	if ok {
		t.Errorf("expected no response for 'a' on unsafe tool, got %v", resp)
	}
}

func runApprovalDiffCmd(t *testing.T, cmd tea.Cmd) approvalDiffComputedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if computed, ok := msg.(approvalDiffComputedMsg); ok {
		return computed
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected approvalDiffComputedMsg or tea.BatchMsg, got %T", msg)
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if computed, ok := c().(approvalDiffComputedMsg); ok {
			return computed
		}
	}
	t.Fatal("expected approvalDiffComputedMsg in batch")
	return approvalDiffComputedMsg{}
}

func TestApprovalRequest_CachesDiffAsync(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: tmp}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{{
		ID:        "1",
		Name:      "write_file",
		Arguments: `{"path":"file.txt","content":"new content\n"}`,
	}}
	updated, cmd := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected command after ApprovalRequestMsg")
	}

	computed := runApprovalDiffCmd(t, cmd)
	if computed.RequestID != 1 {
		t.Errorf("RequestID = %d, want 1", computed.RequestID)
	}
	if computed.CallID != "1" {
		t.Errorf("CallID = %q, want 1", computed.CallID)
	}
	if computed.Diff == "" {
		t.Fatal("expected non-empty diff")
	}

	updated2, _ := model.Update(computed)
	model2 := updated2.(*Model)
	if model2.approvalDiffCallID != "1" {
		t.Errorf("approvalDiffCallID = %q, want 1", model2.approvalDiffCallID)
	}
	if !strings.Contains(model2.approvalDiffContent, "new content") {
		t.Errorf("expected cached diff to contain new content, got %q", model2.approvalDiffContent)
	}
}

func TestApprovalDiffComputed_IgnoresStaleRequestID(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: tmp}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{{
		ID:        "1",
		Name:      "write_file",
		Arguments: `{"path":"file.txt","content":"new content\n"}`,
	}}
	updated, cmd := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)
	stale := runApprovalDiffCmd(t, cmd)

	// A new approval request replaces the active request before the stale diff arrives.
	updated, _ = model.Update(ApprovalRequestMsg{Calls: calls, RequestID: 2})
	model = updated.(*Model)

	updated, _ = model.Update(stale)
	model = updated.(*Model)
	if model.approvalDiffCallID != "" {
		t.Errorf("expected stale diff to be ignored, got callID %q", model.approvalDiffCallID)
	}
	if strings.Contains(model.approvalDiffContent, "new content") {
		t.Error("expected stale diff content to be ignored")
	}
}

func TestApprovalFullscreen_PendingClearedWhenCallAdvances(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: tmp}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()

	calls := []api.ToolCall{
		{ID: "1", Name: "write_file", Arguments: `{"path":"file.txt","content":"new content\n"}`},
		{ID: "2", Name: "read_file", Arguments: `{"path":"file.txt"}`},
	}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	// Request fullscreen diff for the first call; this leaves a pending flag.
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl, Text: "ctrl+e"})
	model = updated.(*Model)
	if model.approvalFullscreenPendingReqID != 1 {
		t.Fatalf("pending reqID = %d, want 1", model.approvalFullscreenPendingReqID)
	}

	// Approve the first call before its diff command returns, advancing to call 2.
	updated, _ = model.Update(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "1"})
	model = updated.(*Model)
	if model.approval.currentIndex() != 1 {
		t.Fatalf("expected controller to advance to second call")
	}

	// Now deliver the diff for the first (no longer current) call.
	computed := runApprovalDiffCmd(t, cmd)
	updated, _ = model.Update(computed)
	model = updated.(*Model)

	if model.approvalFullscreen {
		t.Error("expected fullscreen to stay closed for stale call ID")
	}
	if model.approvalFullscreenPendingReqID != 0 {
		t.Errorf("pending reqID = %d, want 0", model.approvalFullscreenPendingReqID)
	}
	if model.approvalDiffCallID != "" {
		t.Errorf("expected cached diff to remain empty, got %q", model.approvalDiffCallID)
	}
}

func TestApprovalFullscreenDiff_EdgeToEdge(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: tmp}
	m, _ := New(cfg, session, context.Background(), "")
	const width, height = 60, 15
	m.width = width
	m.height = height
	m.updateLayout()

	calls := []api.ToolCall{{
		ID:        "1",
		Name:      "write_file",
		Arguments: `{"path":"file.txt","content":"new content\n"}`,
	}}
	updated, _ := m.Update(ApprovalRequestMsg{Calls: calls, RequestID: 1})
	model := updated.(*Model)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl, Text: "ctrl+e"})
	model = updated.(*Model)
	if model.approvalFullscreen {
		t.Fatal("expected fullscreen to wait for async diff")
	}
	if model.approvalFullscreenPendingReqID != 1 {
		t.Errorf("pending reqID = %d, want 1", model.approvalFullscreenPendingReqID)
	}

	computed := runApprovalDiffCmd(t, cmd)
	updated, _ = model.Update(computed)
	model = updated.(*Model)
	if !model.approvalFullscreen {
		t.Fatal("expected fullscreen diff to open after async diff")
	}

	view := model.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Errorf("fullscreen height = %d, want %d", len(lines), height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("line %d width = %d, want %d", i, w, width)
		}
	}
	if !strings.Contains(view, "Diff preview") {
		t.Errorf("expected fullscreen header, got:\n%s", view)
	}
	if !strings.Contains(view, "Esc or Ctrl+E to close") {
		t.Errorf("expected close footer, got:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(*Model)
	if model.approvalFullscreen {
		t.Error("expected fullscreen diff to close on Esc")
	}
}

func TestSteerOverlay_CursorAndEditing(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.setState(api.TurnStreaming)
	m.steerOpen = true
	m.steerInput = "hello world"
	m.steerCursor = len([]rune(m.steerInput))

	view := m.View().Content
	if !strings.Contains(view, "hello world▌") {
		t.Errorf("expected cursor after input, got:\n%s", view)
	}

	updated := m
	for i := 0; i < 5; i++ {
		u, _ := updated.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		updated = u.(*Model)
	}
	if updated.steerCursor != 6 {
		t.Errorf("cursor = %d, want 6", updated.steerCursor)
	}

	u, _ := updated.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	updated = u.(*Model)
	if updated.steerInput != "hello !world" {
		t.Errorf("steerInput = %q, want hello !world", updated.steerInput)
	}
	if updated.steerCursor != 7 {
		t.Errorf("cursor = %d, want 7", updated.steerCursor)
	}

	u, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	updated = u.(*Model)
	if updated.steerInput != "hello world" {
		t.Errorf("after backspace steerInput = %q, want hello world", updated.steerInput)
	}

	u, _ = updated.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl, Text: "ctrl+w"})
	updated = u.(*Model)
	if updated.steerInput != "world" {
		t.Errorf("after ctrl+w steerInput = %q, want world", updated.steerInput)
	}
	if updated.steerCursor != 0 {
		t.Errorf("after ctrl+w cursor = %d, want 0", updated.steerCursor)
	}

	u, _ = updated.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl, Text: "ctrl+u"})
	updated = u.(*Model)
	if updated.steerInput != "" || updated.steerCursor != 0 {
		t.Errorf("after ctrl+u input = %q cursor = %d, want empty/0", updated.steerInput, updated.steerCursor)
	}

	view = updated.View().Content
	if !strings.Contains(view, "▌") {
		t.Errorf("expected cursor visible after clear, got:\n%s", view)
	}
}
