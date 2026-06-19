package welcome

import (
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestWelcomeView(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(60)
	m.SetData(Data{Directory: "/tmp", SessionID: "abc", ModelName: "kimi", Version: "dev"})
	view := m.View()
	if !strings.Contains(view, "Welcome to Kimi Code!") {
		t.Errorf("missing title: %q", view)
	}
	if !strings.Contains(view, "Directory:") {
		t.Errorf("missing directory: %q", view)
	}
	if !strings.Contains(view, "Session:") {
		t.Errorf("missing session: %q", view)
	}
	if !strings.Contains(view, "Model:") {
		t.Errorf("missing model: %q", view)
	}
	if !strings.Contains(view, "Version:") {
		t.Errorf("missing version: %q", view)
	}
}

func TestWelcomeView_EmptyValues(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(60)
	m.SetData(Data{})
	view := m.View()
	if !strings.Contains(view, "-") {
		t.Errorf("expected placeholder for empty values: %q", view)
	}
}

func TestWelcomeView_ZeroWidth(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(0)
	if m.View() != "" {
		t.Errorf("expected empty view for zero width, got %q", m.View())
	}
}

func TestWelcomeVersionFallback(t *testing.T) {
	v := Version()
	if v == "" {
		t.Error("Version() should never return empty")
	}
}

func TestWelcomeView_TruncatesLongValues(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(30)
	longDir := strings.Repeat("a", 200)
	m.SetData(Data{Directory: longDir, SessionID: "abc", ModelName: "kimi", Version: "dev"})
	view := m.View()
	if strings.Contains(view, longDir) {
		t.Errorf("long directory should be truncated, got %q", view)
	}
	if !strings.Contains(view, "Directory:") {
		t.Errorf("directory label should still be present, got %q", view)
	}
}
