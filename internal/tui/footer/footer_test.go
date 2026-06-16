package footer

import (
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestFooterTwoLines(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "kimi-k2.5", CWD: "/home/user/proj", ContextMax: 100000, ContextUsed: 5000})
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 footer lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "kimi-k2.5") {
		t.Errorf("line 1 missing model name: %q", lines[0])
	}
	if !strings.Contains(lines[1], "context:") {
		t.Errorf("line 2 missing context: %q", lines[1])
	}
}

func TestFooterModeBadge(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", Mode: ModeYolo, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "YOLO") {
		t.Error("expected YOLO badge")
	}
}

func TestFooterAutoBadge(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", Mode: ModeAuto, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "AUTO") {
		t.Error("expected AUTO badge")
	}
}

func TestFooterGitBadge(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", GitBranch: "main", GitDirty: true, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "main*") {
		t.Errorf("expected dirty git badge, got %q", view)
	}
}

func TestFooterStatusText(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", StatusText: "reading files", ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "reading files") {
		t.Errorf("expected status text, got %q", view)
	}
}

func TestFooterState(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", State: api.TurnThinking, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "thinking") {
		t.Errorf("expected thinking state, got %q", view)
	}
}
