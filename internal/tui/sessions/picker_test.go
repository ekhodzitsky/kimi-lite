package sessions

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestPicker_MovementAndSelection(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "s2", Path: "/tmp", UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "s3", Path: "/tmp", UpdatedAt: time.Now().Add(-2 * time.Hour)},
	}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))

	if !p.HasSelection() {
		t.Fatal("expected initial selection")
	}
	if p.Selected().ID != "s1" {
		t.Errorf("initial selection = %q, want s1", p.Selected().ID)
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.Selected().ID != "s2" {
		t.Errorf("after down = %q, want s2", p.Selected().ID)
	}

	done, selected, copyCmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !done || !selected || copyCmd {
		t.Errorf("enter should select, got done=%v selected=%v copy=%v", done, selected, copyCmd)
	}
}

func TestPicker_ToggleAll(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "s2", Path: "/other", UpdatedAt: time.Now()},
	}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	if len(p.filtered) != 1 || p.filtered[0].ID != "s1" {
		t.Fatalf("expected current-path filter, got %v", p.filtered)
	}

	p.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !p.AllMode() {
		t.Error("expected all mode after toggle")
	}
	if len(p.filtered) != 2 {
		t.Errorf("expected 2 sessions in all mode, got %d", len(p.filtered))
	}
}

func TestPicker_Search(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "alpha", Name: "first", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "beta", Name: "second", Path: "/tmp", UpdatedAt: time.Now()},
	}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	p.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	p.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if len(p.filtered) != 1 || p.filtered[0].ID != "beta" {
		t.Errorf("expected beta, got %v", p.filtered)
	}
}

func TestPicker_SearchBackspaceCJK(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "alpha", Name: "first", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "beta", Name: "second", Path: "/tmp", UpdatedAt: time.Now()},
	}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	p.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	p.Update(tea.KeyPressMsg{Code: '世', Text: "世"})
	p.Update(tea.KeyPressMsg{Code: '界', Text: "界"})
	if p.query != "世界" {
		t.Fatalf("expected query %q, got %q", "世界", p.query)
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p.query != "世" {
		t.Errorf("after one backspace expected %q, got %q", "世", p.query)
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p.query != "" {
		t.Errorf("after two backspaces expected empty query, got %q", p.query)
	}
}

func TestPicker_Cancel(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()}}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	done, selected, copyCmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !done || selected || copyCmd {
		t.Errorf("escape should cancel, got done=%v selected=%v copy=%v", done, selected, copyCmd)
	}
}

func TestFormatCard(t *testing.T) {
	t.Parallel()

	p := NewPicker(nil, "/tmp", 80, 30, styles.New("dark"))
	s := api.Session{
		ID:         "abc",
		Name:       "test",
		Path:       "/tmp",
		UpdatedAt:  time.Now(),
		LastPrompt: "hello world",
	}
	card := p.formatCard(s, 80, true)
	if !strings.Contains(card, "test") {
		t.Errorf("missing title: %q", card)
	}
	if !strings.Contains(card, "hello world") {
		t.Errorf("missing last prompt: %q", card)
	}
	if !strings.Contains(card, "←") {
		t.Errorf("missing current-directory marker: %q", card)
	}

	s.Path = "/other"
	cardOther := p.formatCard(s, 80, false)
	if strings.Contains(cardOther, "←") {
		t.Errorf("unexpected current-directory marker for other path: %q", cardOther)
	}
}

func TestPicker_ClearSearchOnFirstEsc(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "alpha", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "beta", Path: "/tmp", UpdatedAt: time.Now()},
	}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	p.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	p.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !p.searching || p.query != "a" {
		t.Fatalf("expected active search, got searching=%v query=%q", p.searching, p.query)
	}

	done, selected, copyCmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if done || selected || copyCmd {
		t.Errorf("first escape should clear search without closing, got done=%v selected=%v copy=%v", done, selected, copyCmd)
	}
	if p.searching || p.query != "" {
		t.Errorf("expected search cleared, got searching=%v query=%q", p.searching, p.query)
	}

	done, selected, copyCmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !done || selected || copyCmd {
		t.Errorf("second escape should close picker, got done=%v selected=%v copy=%v", done, selected, copyCmd)
	}
}

func TestPicker_CopyKey(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()}}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	done, selected, copyCmd := p.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if done || selected || !copyCmd {
		t.Errorf("y should request copy, got done=%v selected=%v copy=%v", done, selected, copyCmd)
	}
}

func TestPicker_PuralizeSessionCount(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()}}
	p := NewPicker(sessions, "/tmp", 80, 24, styles.New("dark"))
	view := p.View()
	if !strings.Contains(view, "1 session") {
		t.Errorf("expected singular '1 session', got: %q", view)
	}

	p.SetSessions([]api.Session{
		{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "s2", Path: "/tmp", UpdatedAt: time.Now()},
	})
	view = p.View()
	if !strings.Contains(view, "2 sessions") {
		t.Errorf("expected plural '2 sessions', got: %q", view)
	}
}

func TestPicker_FooterFitsWidth(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()}}
	p := NewPicker(sessions, "/tmp", 20, 24, styles.New("dark"))
	view := p.View()
	lines := strings.Split(view, "\n")
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Errorf("line exceeds picker width 20: %q", line)
		}
	}
}

func TestTruncateWidth(t *testing.T) {
	t.Parallel()

	if got := truncateWidth("hello", 10); got != "hello" {
		t.Errorf("truncateWidth(hello, 10) = %q, want hello", got)
	}
	if got := truncateWidth("hello world", 5); got != "hell…" {
		t.Errorf("truncateWidth(hello world, 5) = %q, want hell…", got)
	}
	if got := truncateWidth("你好世界", 5); got != "你好…" {
		t.Errorf("truncateWidth(你好世界, 5) = %q, want 你好…", got)
	}
	if got := truncateWidth("anything", 0); got != "" {
		t.Errorf("truncateWidth(anything, 0) = %q, want empty", got)
	}
}

func TestPicker_SelectedCardNoTrailingNewline(t *testing.T) {
	t.Parallel()

	p := NewPicker(nil, "/tmp", 80, 30, styles.New("dark"))
	s := api.Session{ID: "abc", Name: "test", Path: "/tmp", UpdatedAt: time.Now(), LastPrompt: "hello"}
	card := p.formatCard(s, 80, true)
	if strings.HasSuffix(card, "\n") {
		t.Errorf("selected card should not end with newline: %q", card)
	}
}
