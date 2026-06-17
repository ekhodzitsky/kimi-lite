package activity

import (
	"strings"
	"testing"

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
