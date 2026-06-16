// Package welcome renders the empty-state welcome box.
package welcome

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

// Version is the version shown in the welcome panel until build-info wiring is
// in place.
const Version = "0.5.0-dev"

// Data carries the dynamic parts of the welcome box.
type Data struct {
	Directory string
	SessionID string
	ModelName string
	Version   string
}

// Model renders the welcome box.
type Model struct {
	styles *styles.Styles
	width  int
	data   Data
}

// New creates a welcome model.
func New(st *styles.Styles) *Model {
	return &Model{styles: st}
}

// SetSize sets the width.
func (m *Model) SetSize(w int) {
	m.width = w
}

// SetData updates data.
func (m *Model) SetData(d Data) {
	m.data = d
}

// View renders the welcome box.
func (m *Model) View() string {
	if m.width <= 0 {
		return ""
	}
	innerW := m.width - 4
	if innerW < 20 {
		innerW = 20
	}
	title := m.styles.WelcomeTitle.Render("Welcome to Kimi Code!")
	hint := m.styles.WelcomeText.Render("Send /help for help info.")
	lines := []string{
		title,
		hint,
		"",
		m.line("Directory:", m.data.Directory),
		m.line("Session:", m.data.SessionID),
		m.line("Model:", m.data.ModelName),
		m.line("Version:", m.data.Version),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return m.styles.WelcomeBox.Width(innerW).Render(content)
}

func (m *Model) line(label, value string) string {
	if value == "" {
		value = "-"
	}
	return m.styles.WelcomeText.Render(fmt.Sprintf("%-10s %s", label, value))
}
