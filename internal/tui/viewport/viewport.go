// Package viewport provides a scrollable output area for the kimi-lite TUI.
package viewport

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

const (
	pageScrollLines       = 10
	mouseWheelScrollLines = 3
)

// searchMatch identifies a single query occurrence in the rendered transcript.
type searchMatch struct {
	line     int // line index in the full content
	colStart int // grapheme/display-cell start column
	colEnd   int // grapheme/display-cell end column
}

// Model wraps bubbles.Viewport with auto-scroll and indicators.
type Model struct {
	vp            viewport.Model
	styles        *styles.Styles
	autoScroll    bool
	width         int
	height        int
	scrollPercent float64

	searchQuery         string
	searchCaseSensitive bool
	searchMatches       []searchMatch
	searchCurrentIdx    int
	searchMatchStyle    lipgloss.Style
	searchCurrentStyle  lipgloss.Style
}

// New creates a new viewport model.
func New(st *styles.Styles) *Model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// Mouse wheel events are handled explicitly below; disable the underlying
	// viewport handler to avoid double-scrolling.
	vp.MouseWheelEnabled = false
	m := &Model{
		vp:         vp,
		styles:     st,
		autoScroll: true,
	}
	m.SetSearchStyles(st.SearchMatch, st.SearchMatchCurrent)
	return m
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
	content = m.applySearchHighlights(content)
	if !m.scrollIndicatorVisible() || m.height <= 1 {
		return tea.NewView(content)
	}

	indicator := m.scrollIndicator()
	indicatorWidth := ansi.StringWidth(indicator)
	lines := strings.Split(content, "\n")

	// Reserve one line at the bottom for the scroll indicator. The remaining
	// height-1 lines are used for content so the indicator never overwrites it.
	contentLines := m.height - 1
	if len(lines) > contentLines {
		lines = lines[:contentLines]
	} else if len(lines) < contentLines {
		lines = append(lines, make([]string, contentLines-len(lines))...)
	}

	padding := m.width - indicatorWidth
	if padding < 0 {
		padding = 0
	}
	statusLine := strings.Repeat(" ", padding) + indicator
	if ansi.StringWidth(statusLine) > m.width {
		statusLine = ansi.Cut(statusLine, 0, m.width)
	}

	lines = append(lines, statusLine)
	return tea.NewView(strings.Join(lines, "\n"))
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
	m.scrollPercent = m.vp.ScrollPercent()
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

// ScrollToLine scrolls the viewport so the given line is roughly centered.
func (m *Model) ScrollToLine(line int) {
	visibleHeight := m.visibleHeight()
	y := line - visibleHeight/2
	if y < 0 {
		y = 0
	}
	maxY := m.vp.TotalLineCount() - visibleHeight
	if maxY < 0 {
		maxY = 0
	}
	if y > maxY {
		y = maxY
	}
	m.vp.SetYOffset(y)
	m.scrollPercent = m.vp.ScrollPercent()
	m.autoScroll = false
}

// VisibleLineRange returns the start and end line indices (in the full content)
// that are currently visible.
func (m *Model) VisibleLineRange() (start, end int) {
	start = m.vp.YOffset()
	end = start + m.visibleHeight()
	total := m.vp.TotalLineCount()
	if end > total {
		end = total
	}
	return start, end
}

// SearchMatchCount returns the number of active search matches.
func (m *Model) SearchMatchCount() int {
	return len(m.searchMatches)
}

// SetSearchStyles configures the styles used for match highlighting.
func (m *Model) SetSearchStyles(matchStyle, currentStyle lipgloss.Style) {
	m.searchMatchStyle = matchStyle
	m.searchCurrentStyle = currentStyle
}

// SetSearch sets the query and case-sensitivity, recomputes matches, and scrolls
// to the first match. It returns the total number of matches and the current
// match index.
func (m *Model) SetSearch(query string, caseSensitive bool) (int, int) {
	m.searchQuery = query
	m.searchCaseSensitive = caseSensitive
	m.recomputeSearchMatches()
	return len(m.searchMatches), m.searchCurrentIdx
}

// SearchNext advances to the next match and scrolls to it. The second result is
// the total match count; the third reports whether there were any matches.
func (m *Model) SearchNext() (int, int, bool) {
	if len(m.searchMatches) == 0 {
		return -1, 0, false
	}
	m.searchCurrentIdx = (m.searchCurrentIdx + 1) % len(m.searchMatches)
	m.scrollToCurrentMatch()
	return m.searchCurrentIdx, len(m.searchMatches), true
}

// SearchPrevious moves to the previous match and scrolls to it.
func (m *Model) SearchPrevious() (int, int, bool) {
	if len(m.searchMatches) == 0 {
		return -1, 0, false
	}
	m.searchCurrentIdx = (m.searchCurrentIdx - 1 + len(m.searchMatches)) % len(m.searchMatches)
	m.scrollToCurrentMatch()
	return m.searchCurrentIdx, len(m.searchMatches), true
}

// ClearSearch removes the active query, matches, and highlighting.
func (m *Model) ClearSearch() {
	m.searchQuery = ""
	m.searchCaseSensitive = false
	m.searchMatches = nil
	m.searchCurrentIdx = -1
}

// RefreshSearch recomputes matches for the current query without changing the
// current match index when possible. It returns the updated match count and
// current index.
func (m *Model) RefreshSearch() (int, int) {
	oldIdx := m.searchCurrentIdx
	oldLine := -1
	if oldIdx >= 0 && oldIdx < len(m.searchMatches) {
		oldLine = m.searchMatches[oldIdx].line
	}
	m.recomputeSearchMatches()
	if len(m.searchMatches) == 0 {
		m.searchCurrentIdx = -1
		return 0, -1
	}
	if oldIdx < 0 {
		m.searchCurrentIdx = 0
	} else if oldIdx < len(m.searchMatches) && m.searchMatches[oldIdx].line == oldLine {
		// Preserve the previous match if it is still at the same line.
		m.searchCurrentIdx = oldIdx
	} else {
		// Otherwise jump to the nearest match to the previous line.
		m.searchCurrentIdx = m.nearestMatchToLine(oldLine)
		if m.searchCurrentIdx < 0 {
			m.searchCurrentIdx = 0
		}
	}
	return len(m.searchMatches), m.searchCurrentIdx
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

func (m *Model) visibleHeight() int {
	if m.scrollIndicatorVisible() {
		if m.height <= 1 {
			return m.height
		}
		return m.height - 1
	}
	return m.height
}

func (m *Model) nearestMatchToLine(line int) int {
	bestIdx := -1
	bestDist := int(^uint(0) >> 1)
	for i, match := range m.searchMatches {
		dist := match.line - line
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

func (m *Model) recomputeSearchMatches() {
	m.searchMatches = m.searchMatches[:0]
	m.searchCurrentIdx = -1
	if m.searchQuery == "" {
		return
	}

	content := m.vp.GetContent()
	if content == "" {
		return
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		plain := ansi.Strip(line)
		target := plain
		query := m.searchQuery
		if !m.searchCaseSensitive {
			target = strings.ToLower(target)
			query = strings.ToLower(query)
		}

		start := 0
		for {
			idx := strings.Index(target[start:], query)
			if idx == -1 {
				break
			}
			matchStart := start + idx
			matchEnd := matchStart + len(query)
			m.searchMatches = append(m.searchMatches, searchMatch{
				line:     i,
				colStart: ansi.StringWidth(plain[:matchStart]),
				colEnd:   ansi.StringWidth(plain[:matchEnd]),
			})
			start = matchEnd
		}
	}

	if len(m.searchMatches) > 0 {
		m.searchCurrentIdx = 0
		m.scrollToCurrentMatch()
	}
}

func (m *Model) scrollToCurrentMatch() {
	if m.searchCurrentIdx < 0 || m.searchCurrentIdx >= len(m.searchMatches) {
		return
	}
	match := m.searchMatches[m.searchCurrentIdx]
	m.ScrollToLine(match.line)
}

func (m *Model) applySearchHighlights(content string) string {
	if len(m.searchMatches) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	visibleStart := m.vp.YOffset()
	visibleCount := m.visibleHeight()
	if visibleCount > len(lines) {
		visibleCount = len(lines)
	}
	for i := 0; i < visibleCount; i++ {
		lineIdx := visibleStart + i
		var ranges []lipgloss.Range
		for _, match := range m.searchMatches {
			if match.line != lineIdx {
				continue
			}
			style := m.searchMatchStyle
			if m.searchCurrentIdx >= 0 && match == m.searchMatches[m.searchCurrentIdx] {
				style = m.searchCurrentStyle
			}
			ranges = append(ranges, lipgloss.NewRange(match.colStart, match.colEnd, style))
		}
		if len(ranges) > 0 {
			lines[i] = lipgloss.StyleRanges(lines[i], ranges...)
		}
	}
	return strings.Join(lines, "\n")
}
