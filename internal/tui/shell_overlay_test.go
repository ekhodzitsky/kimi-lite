package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/shell"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestShellOverlayToggle(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if m.shellOverlay.IsOpen() {
		t.Fatal("shell overlay should be closed initially")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model := updated.(*Model)
	if !model.shellOverlay.IsOpen() {
		t.Fatal("ctrl+x should open shell overlay")
	}

	updated2, _ := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model2 := updated2.(*Model)
	if model2.shellOverlay.IsOpen() {
		t.Fatal("ctrl+x should close shell overlay")
	}
}

func TestShellOverlayEscClose(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.shellOverlay.Open()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model := updated.(*Model)
	if model.shellOverlay.IsOpen() {
		t.Fatal("esc should close shell overlay")
	}
}

func TestShellOverlayManualApproval(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.approvalMode = approvalModeManual
	m.shellOverlay.Open()

	for _, r := range "echo hi" {
		m.Update(tea.KeyPressMsg{Code: rune(r), Text: string(r)})
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command to request confirmation")
	}
	msg := cmd()
	if _, ok := msg.(shell.ExecuteMsg); !ok {
		t.Fatalf("expected ExecuteMsg, got %T", msg)
	}

	updated, _ = m.Update(msg)
	model := updated.(*Model)
	if !model.shellOverlay.IsConfirming() {
		t.Fatal("manual mode should enter confirmation state")
	}

	// Confirm execution.
	updated2, cmd2 := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd2 == nil {
		t.Fatal("expected command to execute confirmed command")
	}
	msg2 := cmd2()
	if _, ok := msg2.(shell.ConfirmedExecuteMsg); !ok {
		t.Fatalf("expected ConfirmedExecuteMsg, got %T", msg2)
	}
	updated2, _ = model.Update(msg2)
	model2 := updated2.(*Model)
	if model2.shellOverlay.IsConfirming() {
		t.Fatal("confirming should clear after second enter")
	}
}

func TestShellOverlayAutoModeRunsDirectly(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.approvalMode = approvalModeAuto
	m.shellOverlay.Open()

	for _, r := range "echo hi" {
		m.Update(tea.KeyPressMsg{Code: rune(r), Text: string(r)})
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model := updated.(*Model)
	if model.shellOverlay.IsConfirming() {
		t.Fatal("auto mode should not enter confirmation state")
	}
	if cmd == nil {
		t.Fatal("expected command to execute command")
	}
	if _, ok := cmd().(shell.ExecuteMsg); !ok {
		t.Fatalf("expected ExecuteMsg, got %T", cmd())
	}
}

func TestShellOverlayCancelRunning(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.approvalMode = approvalModeAuto
	m.shellOverlay.Open()
	m.shellOverlay.SetRunning("sleep 10", true)

	ctx, cancel := context.WithCancel(context.Background())
	m.shellCancel = cancel

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c while running should return a command")
	}
	msg := cmd()
	if _, ok := msg.(shell.CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", msg)
	}

	updated, _ = m.Update(msg)
	model := updated.(*Model)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled")
	}

	if !model.shellOverlay.IsOpen() {
		t.Fatal("overlay should stay open after canceling a running command")
	}
}

func TestShellOverlayHistory(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.approvalMode = approvalModeAuto
	m.shellOverlay.Open()

	for _, r := range "first" {
		m.Update(tea.KeyPressMsg{Code: rune(r), Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	m.shellOverlay.Open()
	for _, r := range "second" {
		m.Update(tea.KeyPressMsg{Code: rune(r), Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	hist := m.shellOverlay.History()
	if len(hist) != 2 || hist[0] != "first" || hist[1] != "second" {
		t.Fatalf("history = %v, want [first second]", hist)
	}
}

func TestShellResultAppendsMessage(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	before := len(m.messages)

	updated, _ := m.Update(ShellResultMsg{Command: "echo hi", Output: "hi\n", ExitCode: 0})
	model := updated.(*Model)
	if len(model.messages) != before+1 {
		t.Fatalf("expected one message appended, got %d", len(model.messages))
	}
	if !stringsContains(model.messages[before].View().Content, "shell$ echo hi") {
		t.Fatalf("expected message to contain command, got %q", model.messages[before].View().Content)
	}
}

func TestShellProgressUpdatesActivity(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.shellOverlay.SetRunning("echo hi", true)
	m.Update(ShellProgressMsg{Chunk: "hi\n"})

	if m.shellOutput != "hi\n" {
		t.Fatalf("shellOutput = %q, want hi\\n", m.shellOutput)
	}
	m.activity.SetSize(80)
	view := m.activity.View()
	if !stringsContains(view, "hi") {
		t.Fatalf("activity view should contain shell output, got %q", view)
	}
}

func newTestModel(t *testing.T) *Model {
	t.Helper()
	cfg := &api.Config{
		LLM: api.LLMConfig{Model: "test"},
		Session: api.SessionConfig{
			DBPath: ":memory:",
		},
	}
	path := t.TempDir()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create session path: %v", err)
	}
	session := &api.Session{Path: path}
	m, err := New(cfg, session, context.Background(), "")
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	m.width = 80
	m.height = 24
	m.updateLayout()
	return m
}

func stringsContains(s, substr string) bool {
	return strings.Contains(s, substr)
}
