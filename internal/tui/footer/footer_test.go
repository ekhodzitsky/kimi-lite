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

func TestFooterGitBadge_NoAheadBehind(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", GitBranch: "main", ContextMax: 1})
	view := m.View()
	// Ahead/behind counters would render as " 0/0" after the branch name.
	if strings.Contains(view, " 0/0") || strings.Contains(view, " 1/1") {
		t.Errorf("footer should not render ahead/behind counters, got %q", view)
	}
}

func TestFooterToolCount(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", ToolCount: 3, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "tools: 3") {
		t.Errorf("expected tool count on line 2, got %q", view)
	}
}

func TestFooterToolCount_ZeroHidden(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", ToolCount: 0, ContextMax: 1})
	view := m.View()
	if strings.Contains(view, "tools:") {
		t.Errorf("zero tool count should be hidden, got %q", view)
	}
}

func TestFooterManualBadge(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", Mode: ModeManual, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "MANUAL") {
		t.Errorf("expected MANUAL badge, got %q", view)
	}
}

func TestFooterPlanBadge(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", Mode: ModeAuto, PlanMode: true, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "PLAN") {
		t.Errorf("expected PLAN badge, got %q", view)
	}
	if !strings.Contains(view, "AUTO") {
		t.Errorf("expected AUTO badge alongside PLAN, got %q", view)
	}
}

func TestFooterPlanBadge_WithManual(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", Mode: ModeManual, PlanMode: true, ContextMax: 1})
	view := m.View()
	if !strings.Contains(view, "PLAN") {
		t.Errorf("expected PLAN badge, got %q", view)
	}
	if !strings.Contains(view, "MANUAL") {
		t.Errorf("expected MANUAL badge alongside PLAN, got %q", view)
	}
}

func TestFooterContextClamp(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{ModelName: "m", ContextMax: 100, ContextUsed: 250})
	view := m.View()
	if !strings.Contains(view, "100.0%+") {
		t.Errorf("expected clamped context overflow indicator, got %q", view)
	}
}
