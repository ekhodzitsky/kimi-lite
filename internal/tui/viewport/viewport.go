// Package viewport provides a scrollable output area for the kimi-lite TUI.
package viewport

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

const (
	pageScrollLines       = 10
	mouseWheelScrollLines = 3
)

// Model wraps bubbles.Viewport with auto-scroll and indicators.
type Model struct {
	vp            viewport.Model
	styles        *styles.Styles
	autoScroll    bool
	width         int
	height        int
	scrollPercent float64
}

// New creates a new viewport model.
func New(st *styles.Styles) *Model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.MouseWheelEnabled = true
	return &Model{
		vp:         vp,
		styles:     st,
		autoScroll: true,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.UpdateMsg(msg)
	return m, cmd
}

// UpdateMsg processes a message and returns the resulting command.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "pgup":
			m.vp.ScrollUp(pageScrollLines)
			m.autoScroll = false
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		case "pgdown":
			m.vp.ScrollDown(pageScrollLines)
			m.checkAutoScroll()
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		case "home":
			m.vp.GotoTop()
			m.autoScroll = false
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		case "end":
			m.vp.GotoBottom()
			m.autoScroll = true
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		case "up":
			m.vp.ScrollUp(1)
			m.autoScroll = false
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		case "down":
			m.vp.ScrollDown(1)
			m.checkAutoScroll()
			m.scrollPercent = m.vp.ScrollPercent()
			return tea.Batch(cmds...)
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.vp.ScrollUp(mouseWheelScrollLines)
			m.autoScroll = false
		} else if msg.Button == tea.MouseWheelDown {
			m.vp.ScrollDown(mouseWheelScrollLines)
			m.checkAutoScroll()
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	m.scrollPercent = m.vp.ScrollPercent()
	return tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	content := m.vp.View()
	if m.scrollIndicatorVisible() {
		indicator := m.scrollIndicator()
		lines := strings.Split(content, "\n")
		for i := range lines {
			if i == len(lines)-1 {
				lines[i] = indicator
			}
		}
		content = strings.Join(lines, "\n")
	}
	return tea.NewView(content)
}

// SetSize sets the viewport dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.vp.SetWidth(w)
	m.vp.SetHeight(h)
}

// SetContent sets the entire content string.
func (m *Model) SetContent(s string) {
	m.vp.SetContent(s)
	if m.autoScroll {
		m.vp.GotoBottom()
	}
}

// ScrollPercent returns the current scroll percentage.
func (m *Model) ScrollPercent() float64 {
	return m.scrollPercent
}

// AtBottom reports whether the viewport is scrolled to the bottom.
func (m *Model) AtBottom() bool {
	return m.vp.AtBottom()
}

// GotoBottom scrolls to the bottom and enables auto-scroll.
func (m *Model) GotoBottom() {
	m.vp.GotoBottom()
	m.scrollPercent = m.vp.ScrollPercent()
	m.autoScroll = true
}

// GotoTop scrolls to the top and disables auto-scroll.
func (m *Model) GotoTop() {
	m.vp.GotoTop()
	m.scrollPercent = m.vp.ScrollPercent()
	m.autoScroll = false
}

func (m *Model) checkAutoScroll() {
	m.autoScroll = m.vp.AtBottom()
}

func (m *Model) scrollIndicatorVisible() bool {
	return !m.vp.AtBottom()
}

func (m *Model) scrollIndicator() string {
	percent := int(m.scrollPercent * 100)
	return m.styles.ScrollIndicator.Render(fmt.Sprintf("▼ %d%%", percent))
}
