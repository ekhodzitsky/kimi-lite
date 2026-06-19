// Package activity renders transient status between the transcript and input.
package activity

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	// Reserve cells consumed by the Activity style (MarginLeft 1 + Padding 0,1
	// = 3) plus the spinner and the separating space.
	line = m.fit(line, m.width-3-ansi.StringWidth(m.spinner.View())-1)
	var b strings.Builder
	b.WriteString(m.styles.Activity.Render(m.spinner.View() + " " + line))
	if m.data.State == api.TurnToolCalls && len(m.data.ToolCalls) > 0 {
		for _, tc := range m.data.ToolCalls {
			b.WriteString("\n")
			toolLine := "  • " + tc.Name
			// ActivityTool has MarginLeft(2).
			b.WriteString(m.styles.ActivityTool.Render(m.fit(toolLine, m.width-2)))
			if out := m.data.ToolOutputs[tc.ID]; out != "" {
				lines := strings.Split(out, "\n")
				tail := lines
				if len(tail) > 4 {
					tail = tail[len(tail)-4:]
				}
				for _, outLine := range tail {
					b.WriteString("\n")
					rendered := "    " + outLine
					// ActivityOutput has MarginLeft(2).
					b.WriteString(m.styles.ActivityOutput.Render(m.fit(rendered, m.width-2)))
				}
			}
		}
	}
	return b.String()
}

// fit truncates s to w display cells, preserving an overflow indicator when
// truncation occurs.
func (m *Model) fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Cut(s, 0, w-1) + "…"
}

// Height returns the rendered height of the panel.
func (m *Model) Height() int {
	return lipgloss.Height(m.View())
}

func (m *Model) activeLine() (active bool, line string) {
	switch m.data.State {
	case api.TurnThinking:
		return true, m.statusOr("thinking...")
	case api.TurnStreaming:
		return true, m.statusOr("streaming...")
	case api.TurnToolCalls:
		return true, m.statusOr("running tools...")
	case api.TurnWaitingApproval:
		return true, m.statusOr("waiting for approval...")
	case api.TurnWaitingPlan:
		return true, m.statusOr("waiting for plan approval...")
	default:
		return false, ""
	}
}

func (m *Model) statusOr(fallback string) string {
	if m.data.StatusText != "" {
		return m.data.StatusText
	}
	return fallback
}
