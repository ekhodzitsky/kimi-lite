package activity

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestActivityIdle(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{State: api.TurnIdle})
	if m.View() != "" {
		t.Errorf("expected empty view for idle state, got %q", m.View())
	}
}

func TestActivityThinking(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{State: api.TurnThinking})
	view := m.View()
	if !strings.Contains(view, "thinking...") {
		t.Errorf("missing thinking text: %q", view)
	}
}

func TestActivityToolCalls(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{
		State:     api.TurnToolCalls,
		ToolCalls: []api.ToolCall{{Name: "read_file"}, {Name: "grep"}},
	})
	view := m.View()
	if !strings.Contains(view, "running tools...") {
		t.Errorf("missing running tools text: %q", view)
	}
	if !strings.Contains(view, "read_file") || !strings.Contains(view, "grep") {
		t.Errorf("missing tool names: %q", view)
	}
}

func TestActivityToolOutput(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{
		State:       api.TurnToolCalls,
		ToolCalls:   []api.ToolCall{{ID: "c1", Name: "shell"}},
		ToolOutputs: map[string]string{"c1": "line1\nline2\nline3"},
	})
	view := m.View()
	if !strings.Contains(view, "line3") {
		t.Errorf("missing output tail: %q", view)
	}
}

func TestActivityStatusText(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{State: api.TurnThinking, StatusText: "reading context"})
	view := m.View()
	if !strings.Contains(view, "reading context") {
		t.Errorf("expected custom status text, got %q", view)
	}
	if strings.Contains(view, "thinking...") {
		t.Errorf("custom status should override default, got %q", view)
	}
}

func TestActivityWaitingApproval(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{State: api.TurnWaitingApproval})
	view := m.View()
	if !strings.Contains(view, "waiting for approval") {
		t.Errorf("expected approval waiting text, got %q", view)
	}
}

func TestActivityWaitingPlan(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(80)
	m.SetData(Data{State: api.TurnWaitingPlan})
	view := m.View()
	if !strings.Contains(view, "waiting for plan approval") {
		t.Errorf("expected plan approval waiting text, got %q", view)
	}
}

func TestActivityWidthTruncation(t *testing.T) {
	st := styles.New("dark")
	m := New(st)
	m.SetSize(20)
	longLine := strings.Repeat("a", 100)
	m.SetData(Data{
		State:       api.TurnToolCalls,
		ToolCalls:   []api.ToolCall{{ID: "c1", Name: "shell"}},
		ToolOutputs: map[string]string{"c1": longLine},
	})
	view := m.View()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > 20 {
			t.Errorf("line %d exceeds panel width: width=%d, line=%q", i, got, line)
		}
	}
}
