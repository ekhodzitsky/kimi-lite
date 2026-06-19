// Package welcome renders the empty-state welcome box.
package welcome

import (
	"fmt"
	"runtime/debug"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

// Version returns the build version, falling back to "dev" when build info is
// unavailable (for example, when running tests or an unversioned binary).
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

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
		m.line("Directory:", m.data.Directory, innerW),
		m.line("Session:", m.data.SessionID, innerW),
		m.line("Model:", m.data.ModelName, innerW),
		m.line("Version:", m.data.Version, innerW),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return m.styles.WelcomeBox.Width(innerW).Render(content)
}

func (m *Model) line(label, value string, innerW int) string {
	if value == "" {
		value = "-"
	}
	prefix := fmt.Sprintf("%-10s ", label)
	maxValueW := innerW - ansi.StringWidth(prefix)
	if maxValueW < 0 {
		maxValueW = 0
	}
	if ansi.StringWidth(value) > maxValueW {
		value = ansi.Cut(value, 0, maxValueW-1) + "…"
	}
	return m.styles.WelcomeText.Render(prefix + value)
}
