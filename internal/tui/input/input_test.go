package input

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNew(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)

	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.styles == nil {
		t.Error("styles not set")
	}
	if m.Value() != "" {
		t.Errorf("initial value = %q, want empty", m.Value())
	}
}

func TestInit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command")
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("hello world")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected a command after sending")
	}

	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if msg.Content != "hello world" {
		t.Errorf("SendMsg.Content = %q, want %q", msg.Content, "hello world")
	}

	inp := updated.(*Model)
	if inp.Value() != "" {
		t.Error("input should be cleared after send")
	}
}

func TestSendEmptyMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("   ")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Error("sending empty message should not produce a command")
	}

	inp := updated.(*Model)
	if inp.Value() != "   " {
		t.Error("input should not be cleared for empty send")
	}
}

func TestNewline(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("line1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	if cmd != nil {
		t.Error("newline should not produce a command")
	}

	inp := updated.(*Model)
	if !strings.Contains(inp.Value(), "\n") {
		t.Error("newline should insert a newline character")
	}
}

func TestHistoryNavigationUpDown(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	// Send three messages
	for _, content := range []string{"first", "second", "third"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}

	// Press up - should show "third"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "third" {
		t.Errorf("after up: value = %q, want %q", m.Value(), "third")
	}

	// Press up again - should show "second"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "second" {
		t.Errorf("after up: value = %q, want %q", m.Value(), "second")
	}

	// Press down - should show "third"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "third" {
		t.Errorf("after down: value = %q, want %q", m.Value(), "third")
	}

	// Press down again - should show draft (empty)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "" {
		t.Errorf("after down to draft: value = %q, want empty", m.Value())
	}
}

func TestHistoryPreservesDraft(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("sent")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	m.SetValue("draft")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "sent" {
		t.Errorf("history up: value = %q, want %q", m.Value(), "sent")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "draft" {
		t.Errorf("history down should restore draft, got %q", m.Value())
	}
}

func TestHistoryCap(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 3)
	m.SetWidth(80)

	for _, content := range []string{"a", "b", "c", "d"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}
	if m.history[0] != "b" || m.history[1] != "c" || m.history[2] != "d" {
		t.Errorf("history should keep newest entries, got %v", m.history)
	}
}

func TestHistoryDedupConsecutive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	for _, content := range []string{"hello", "hello", "world", "world", "world"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 2 {
		t.Fatalf("history length = %d, want 2", len(m.history))
	}
	if m.history[0] != "hello" || m.history[1] != "world" {
		t.Errorf("history should de-duplicate consecutive sends, got %v", m.history)
	}
}

func TestHistoryNoDedupNonConsecutive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	for _, content := range []string{"hello", "world", "hello"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}
}

func TestHistoryCapZeroUnbounded(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 0)
	m.SetWidth(80)

	for i := 0; i < 5; i++ {
		m.SetValue("msg")
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 1 {
		t.Fatalf("history length = %d, want 1 (consecutive dedup)", len(m.history))
	}

	for i := 0; i < 5; i++ {
		m.SetValue(string(rune('a' + i)))
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 6 {
		t.Errorf("history length with cap=0 = %d, want 6", len(m.history))
	}
}

func TestFocusBlur(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)

	cmd := m.Focus()
	if cmd == nil {
		t.Error("Focus() should return a command")
	}

	m.Blur()
	// Blur doesn't panic - that's the main test
}

func TestReset(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("something")
	m.Reset()
	if m.Value() != "" {
		t.Errorf("after Reset(): value = %q, want empty", m.Value())
	}
}

func TestConfigurableKeyMap(t *testing.T) {
	t.Parallel()

	cfg := api.KeybindingConfig{
		Send:    "ctrl+s",
		Newline: "ctrl+j",
	}
	km := ConfigurableKeyMap(cfg)

	if len(km.Send.Keys()) != 1 || km.Send.Keys()[0] != "ctrl+s" {
		t.Errorf("Send keys = %v, want [ctrl+s]", km.Send.Keys())
	}
	if len(km.Newline.Keys()) != 1 || km.Newline.Keys()[0] != "ctrl+j" {
		t.Errorf("Newline keys = %v, want [ctrl+j]", km.Newline.Keys())
	}
}

func TestConfigurableKeyMapHelp(t *testing.T) {
	t.Parallel()

	cfg := api.KeybindingConfig{
		Send:    "ctrl+s",
		Newline: "ctrl+j",
	}
	km := ConfigurableKeyMap(cfg)

	if km.Send.Help().Desc == "" {
		t.Error("Send binding should have non-empty help description")
	}
	if km.Newline.Help().Desc == "" {
		t.Error("Newline binding should have non-empty help description")
	}
}

func TestSetWidth(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(100)
	view := m.View()
	if view == "" {
		t.Error("View() should not be empty after setting width")
	}
}
