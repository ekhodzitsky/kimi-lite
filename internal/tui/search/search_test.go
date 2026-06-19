package search

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func newTestSearch() *Model {
	return New(styles.New("dark"))
}

func TestNew(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	if m.IsOpen() {
		t.Error("new search model should be closed")
	}
	if m.Query() != "" {
		t.Errorf("new query = %q, want empty", m.Query())
	}
	if m.CaseSensitive() {
		t.Error("new search should be case-insensitive")
	}
}

func TestOpenClose(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	if !m.IsOpen() {
		t.Error("Open() should open the overlay")
	}

	m.SetMatches(5, 2)
	m.Close()
	if m.IsOpen() {
		t.Error("Close() should close the overlay")
	}
	if m.Query() != "" {
		t.Errorf("Close() should clear query, got %q", m.Query())
	}
	if m.MatchCount() != 0 {
		t.Errorf("Close() should reset match count, got %d", m.MatchCount())
	}
}

func TestTypingUpdatesQuery(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()

	cmd := m.UpdateMsg(tea.KeyPressMsg{Text: "h"})
	if got := m.Query(); got != "h" {
		t.Errorf("query = %q, want %q", got, "h")
	}
	if cmd == nil {
		t.Fatal("typing should emit a command")
	}
	if _, ok := cmd().(QueryChangedMsg); !ok {
		t.Errorf("expected QueryChangedMsg, got %T", cmd())
	}
}

func TestCursorMovement(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	m.UpdateMsg(tea.KeyPressMsg{Text: "abc"})

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	// Cannot move before start.
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
}

func TestBackspace(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	m.UpdateMsg(tea.KeyPressMsg{Text: "abc"})
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyLeft})

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.Query() != "ac" {
		t.Errorf("query = %q, want %q", m.Query(), "ac")
	}
	if _, ok := cmd().(QueryChangedMsg); !ok {
		t.Errorf("expected QueryChangedMsg, got %T", cmd())
	}
}

func TestToggleCase(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.CaseSensitive() {
		t.Error("Ctrl+O should enable case-sensitive mode")
	}
	if _, ok := cmd().(QueryChangedMsg); !ok {
		t.Errorf("expected QueryChangedMsg, got %T", cmd())
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.CaseSensitive() {
		t.Error("second Ctrl+O should disable case-sensitive mode")
	}
}

func TestNavigationMessages(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()

	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want any
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, NextMsg{}},
		{"ctrl+n", tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, NextMsg{}},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, NextMsg{}},
		{"shift+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, PreviousMsg{}},
		{"ctrl+p", tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, PreviousMsg{}},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, PreviousMsg{}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEsc}, CloseMsg{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := m.UpdateMsg(tc.key)
			if cmd == nil {
				t.Fatal("expected a command")
			}
			got := cmd()
			if got == nil {
				t.Fatal("command returned nil")
			}
			switch tc.want.(type) {
			case NextMsg:
				if _, ok := got.(NextMsg); !ok {
					t.Errorf("expected NextMsg, got %T", got)
				}
			case PreviousMsg:
				if _, ok := got.(PreviousMsg); !ok {
					t.Errorf("expected PreviousMsg, got %T", got)
				}
			case CloseMsg:
				if _, ok := got.(CloseMsg); !ok {
					t.Errorf("expected CloseMsg, got %T", got)
				}
			}
		})
	}
}

func TestViewCounter(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	m.SetSize(80)
	m.SetMatches(12, 2)
	m.query = "foo"

	view := m.View().Content
	if !contains(view, "3/12") {
		t.Errorf("View should contain counter %q, got %q", "3/12", view)
	}
}

func TestViewNoMatches(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	m.SetSize(80)
	m.query = "foo"
	m.SetMatches(0, -1)

	view := m.View().Content
	if !contains(view, "no matches") {
		t.Errorf("View should contain %q, got %q", "no matches", view)
	}
}

func TestViewEmptyQuery(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.Open()
	m.SetSize(80)
	m.SetMatches(5, 0)

	view := m.View().Content
	if contains(view, "no matches") || contains(view, "/") {
		t.Errorf("View should not show counter for empty query, got %q", view)
	}
}

func TestViewClosed(t *testing.T) {
	t.Parallel()

	m := newTestSearch()
	m.SetSize(80)
	if m.View().Content != "" {
		t.Errorf("closed View should be empty, got %q", m.View().Content)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
