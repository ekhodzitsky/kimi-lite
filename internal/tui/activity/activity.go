// Package activity renders transient status between the transcript and input.
package activity

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// Data is the snapshot needed to render the activity panel.
type Data struct {
	State       api.TurnState
	StatusText  string
	ToolCalls   []api.ToolCall
	ToolOutputs map[string]string // callID -> live output tail
}

// Model renders transient activity status.
type Model struct {
	styles  *styles.Styles
	width   int
	data    Data
	spinner spinner.Model
}

// New creates an activity model.
func New(st *styles.Styles) *Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return &Model{
		styles:  st,
		spinner: s,
	}
}

// SetSize sets the panel width.
func (m *Model) SetSize(w int) {
	m.width = w
}

// SetData updates the activity data.
func (m *Model) SetData(d Data) {
	m.data = d
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return m.spinner.Tick
}

// UpdateMsg processes messages and returns the spinner tick command.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

// View renders the activity panel or an empty string when idle.
func (m *Model) View() string {
	if m.width <= 0 {
		return ""
	}
	active, line := m.activeLine()
	if !active {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.styles.Activity.Render(m.spinner.View() + " " + line))
	if m.data.State == api.TurnToolCalls && len(m.data.ToolCalls) > 0 {
		for _, tc := range m.data.ToolCalls {
			b.WriteString("\n")
			b.WriteString(m.styles.ActivityTool.Render("  • " + tc.Name))
			if out := m.data.ToolOutputs[tc.ID]; out != "" {
				lines := strings.Split(out, "\n")
				tail := lines
				if len(tail) > 4 {
					tail = tail[len(tail)-4:]
				}
				for _, line := range tail {
					b.WriteString("\n")
					b.WriteString(m.styles.ActivityOutput.Render("    " + line))
				}
			}
		}
	}
	return b.String()
}

// Height returns the rendered height of the panel.
func (m *Model) Height() int {
	return lipgloss.Height(m.View())
}

func (m *Model) activeLine() (active bool, line string) {
	switch m.data.State {
	case api.TurnThinking:
		return true, "thinking..."
	case api.TurnStreaming:
		return true, "streaming..."
	case api.TurnToolCalls:
		return true, "running tools..."
	default:
		return false, ""
	}
}
