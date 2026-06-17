package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestHelpView(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(60, 20)
	m.SetData(Data{
		Shortcuts: []Shortcut{{Keys: "enter", Description: "send"}},
		Commands:  []SlashCommand{{Name: "/help", Description: "show help"}},
	})
	view := m.View().Content
	if !strings.Contains(view, "Keyboard shortcuts") {
		t.Errorf("missing title: %q", view)
	}
	if !strings.Contains(view, "/help") {
		t.Errorf("missing command: %q", view)
	}
}

func TestHelpNavigation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(30, 8)
	m.SetData(Data{
		Shortcuts: []Shortcut{
			{Keys: "enter", Description: "send"},
			{Keys: "alt+enter", Description: "newline"},
			{Keys: "tab", Description: "focus"},
			{Keys: "ctrl+g", Description: "editor"},
		},
		Commands: []SlashCommand{
			{Name: "/compact", Description: "compact"},
			{Name: "/clear", Description: "clear"},
			{Name: "/help", Description: "help"},
		},
	})

	if m.offset != 0 {
		t.Fatalf("initial offset = %d, want 0", m.offset)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.offset <= 0 {
		t.Errorf("offset should increase after down, got %d", m.offset)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.offset != 0 {
		t.Errorf("offset should clamp to 0 after pgup, got %d", m.offset)
	}
}

func TestCloseKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"esc", "enter", "q"} {
		if !CloseKeys(key) {
			t.Errorf("CloseKeys(%q) = false, want true", key)
		}
	}
	if CloseKeys("x") {
		t.Error("CloseKeys(\"x\") = true, want false")
	}
}
