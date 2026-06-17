// Package help provides the /help overlay for the kimi-lite TUI.
package help

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

// Shortcut is a single keyboard shortcut entry.
type Shortcut struct {
	Keys        string
	Description string
}

// SlashCommand is a slash command entry.
type SlashCommand struct {
	Name        string
	Description string
}

// Data carries the content of the help panel.
type Data struct {
	Shortcuts []Shortcut
	Commands  []SlashCommand
}

// Model is the help overlay.
type Model struct {
	styles *styles.Styles
	width  int
	height int
	data   Data
	offset int
}

// New creates a help model.
func New(st *styles.Styles) *Model {
	return &Model{styles: st}
}

// SetSize sets the overlay size.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetData updates help content.
func (m *Model) SetData(d Data) {
	m.data = d
	m.offset = 0
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, m.UpdateMsg(msg)
}

// UpdateMsg handles keyboard navigation.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.offset--
		case "down":
			m.offset++
		case "pgup":
			m.offset -= 10
		case "pgdown":
			m.offset += 10
		case "home":
			m.offset = 0
		case "end":
			m.offset = 99999
		}
		m.clampOffset()
	}
	return nil
}

func (m *Model) clampOffset() {
	maxOffset := max(0, m.contentLines()-m.visibleLines())
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

// Offset returns the current scroll offset.
func (m *Model) Offset() int {
	return m.offset
}

func (m *Model) visibleLines() int {
	if m.height < 6 {
		return 1
	}
	return m.height - 6
}

func (m *Model) contentLines() int {
	return lipgloss.Height(m.renderContent())
}

// CloseKeys reports whether the given key string should close the overlay.
func CloseKeys(key string) bool {
	switch key {
	case "esc", "enter", "q":
		return true
	}
	return false
}

// View renders the help overlay.
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		return tea.NewView("")
	}
	innerW := m.width - 4
	innerH := m.height - 4
	if innerW < 20 {
		innerW = 20
	}
	if innerH < 5 {
		innerH = 5
	}
	content := m.renderContent()
	lines := strings.Split(content, "\n")
	end := m.offset + innerH
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[m.offset:end], "\n")
	if m.offset > 0 {
		visible = "▲ more\n" + visible
	}
	if end < len(lines) {
		visible = visible + "\n▼ more"
	}
	return tea.NewView(m.styles.HelpOverlay.Width(innerW).Height(innerH).Render(visible))
}

func (m *Model) renderContent() string {
	var b strings.Builder
	b.WriteString(m.styles.HelpTitle.Render("Keyboard shortcuts") + "\n\n")
	for _, s := range m.data.Shortcuts {
		b.WriteString(m.styles.HelpKey.Render(s.Keys))
		b.WriteString("  " + m.styles.HelpDesc.Render(s.Description) + "\n")
	}
	b.WriteString("\n" + m.styles.HelpTitle.Render("Slash commands") + "\n\n")
	for _, c := range m.data.Commands {
		b.WriteString(m.styles.HelpCommand.Render(c.Name) + "\n")
		b.WriteString("  " + m.styles.HelpDesc.Render(c.Description) + "\n")
	}
	return b.String()
}
