package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// runCmd executes a tea.Cmd and feeds the resulting message back into m.Update,
// returning the updated model. It is useful in tests where commands represent
// events that would be processed before the next keystroke in a real program.
func runCmd(m *Model, cmd tea.Cmd) *Model {
	if cmd == nil {
		return m
	}
	updated, next := m.Update(cmd())
	m = updated.(*Model)
	for next != nil {
		msg := next()
		if msg == nil {
			break
		}
		updated, next = m.Update(msg)
		m = updated.(*Model)
	}
	return m
}

func TestCtrlFTogglesSearch(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.addMessage(msgcomp.NewUserMessage("hello world", m.styles))
	m.updateLayout()

	if m.search.IsOpen() {
		t.Fatal("search should start closed")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	model := updated.(*Model)
	if !model.search.IsOpen() {
		t.Fatal("Ctrl+F should open search")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	model = updated.(*Model)
	if model.search.IsOpen() {
		t.Fatal("second Ctrl+F should close search")
	}
}

func TestSearchKeysDoNotLeak(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	// Open search.
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})

	// Type a key that would normally insert text into the input.
	before := m.input.Value()
	updated, _ := m.Update(tea.KeyPressMsg{Text: "h"})
	model := updated.(*Model)

	if model.input.Value() != before {
		t.Errorf("input value changed while search is open: %q", model.input.Value())
	}
	if model.search.Query() != "h" {
		t.Errorf("search query = %q, want %q", model.search.Query(), "h")
	}
}

func TestSearchNavigation(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.addMessage(msgcomp.NewUserMessage("one two one three", m.styles))
	m.updateLayout()

	// Open search.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = updated.(*Model)

	// Type query and process the resulting QueryChangedMsg.
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "o"})
	m = runCmd(updated.(*Model), cmd)
	updated, cmd = m.Update(tea.KeyPressMsg{Text: "n"})
	m = runCmd(updated.(*Model), cmd)
	updated, cmd = m.Update(tea.KeyPressMsg{Text: "e"})
	m = runCmd(updated.(*Model), cmd)

	if m.search.Query() != "one" {
		t.Fatalf("query = %q, want %q", m.search.Query(), "one")
	}

	// Move to next match.
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = runCmd(updated.(*Model), cmd)
	if m.search.MatchIndex() != 1 {
		t.Errorf("match index = %d, want 1", m.search.MatchIndex())
	}

	// Move to previous match.
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = runCmd(updated.(*Model), cmd)
	if m.search.MatchIndex() != 0 {
		t.Errorf("match index = %d, want 0", m.search.MatchIndex())
	}
}

func TestEscClosesSearch(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Text: "hello"})

	// Esc closes search and emits CloseMsg.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("expected CloseMsg command")
	}

	// Process CloseMsg to fully clear state.
	m = runCmd(m, cmd)
	if m.search.IsOpen() {
		t.Error("Esc should close search")
	}
	if m.vp.SearchMatchCount() != 0 {
		t.Error("viewport highlights should be cleared")
	}
}
