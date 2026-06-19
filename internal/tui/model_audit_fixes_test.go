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
