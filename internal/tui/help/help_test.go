package help

import (
	"fmt"
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

func TestVisibleLines_ReservesIndicatorSpace(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(60, 20) // innerH = 16
	if got := m.visibleLines(); got != 14 {
		t.Errorf("visibleLines = %d, want 14", got)
	}

	m.SetSize(60, 6) // innerH clamped to 5
	if got := m.visibleLines(); got != 3 {
		t.Errorf("visibleLines = %d, want 3", got)
	}
}

func TestHelpView_IndicatorsDoNotOverflowFrame(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(40, 12) // innerH = 8, visibleLines = 6

	var shortcuts []Shortcut
	for i := 0; i < 20; i++ {
		shortcuts = append(shortcuts, Shortcut{Keys: fmt.Sprintf("k%d", i), Description: "desc"})
	}
	m.SetData(Data{Shortcuts: shortcuts})
	m.offset = 5

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("rendered height = %d, want %d", len(lines), m.height)
	}
	if !strings.Contains(view, "▲ more") {
		t.Error("expected top scroll indicator")
	}
	if !strings.Contains(view, "▼ more") {
		t.Error("expected bottom scroll indicator")
	}
}
