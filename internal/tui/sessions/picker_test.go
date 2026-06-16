package sessions

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestPicker_MovementAndSelection(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "s2", Path: "/tmp", UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "s3", Path: "/tmp", UpdatedAt: time.Now().Add(-2 * time.Hour)},
	}
	p := NewPicker(sessions, "/tmp", 80, 24)

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

	done, selected := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !done || !selected {
		t.Errorf("enter should select, got done=%v selected=%v", done, selected)
	}
}

func TestPicker_ToggleAll(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{
		{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()},
		{ID: "s2", Path: "/other", UpdatedAt: time.Now()},
	}
	p := NewPicker(sessions, "/tmp", 80, 24)
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
	p := NewPicker(sessions, "/tmp", 80, 24)
	p.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	p.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if len(p.filtered) != 1 || p.filtered[0].ID != "beta" {
		t.Errorf("expected beta, got %v", p.filtered)
	}
}

func TestPicker_Cancel(t *testing.T) {
	t.Parallel()

	sessions := []api.Session{{ID: "s1", Path: "/tmp", UpdatedAt: time.Now()}}
	p := NewPicker(sessions, "/tmp", 80, 24)
	done, selected := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !done || selected {
		t.Errorf("escape should cancel, got done=%v selected=%v", done, selected)
	}
}
