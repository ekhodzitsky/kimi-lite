// Package search provides the transcript search overlay for the kimi-lite TUI.
package search

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

// QueryChangedMsg is emitted when the search query or case-sensitivity changes.
type QueryChangedMsg struct {
	Query         string
	CaseSensitive bool
}

// NextMsg requests navigation to the next search match.
type NextMsg struct{}

// PreviousMsg requests navigation to the previous search match.
type PreviousMsg struct{}

// CloseMsg requests that the search overlay be closed.
type CloseMsg struct{}

// Model is the search overlay component.
type Model struct {
	styles *styles.Styles

	open          bool
	query         string
	cursor        int // cursor position in runes
	caseSensitive bool
	matchCount    int
	matchIndex    int // 0-based, or -1 when there are no matches
	width         int
}

// New creates a new search overlay model.
func New(st *styles.Styles) *Model {
	return &Model{
		styles:     st,
		matchIndex: -1,
	}
}

// Open shows the search overlay.
func (m *Model) Open() {
	m.open = true
}

// Close hides the search overlay and resets draft state.
func (m *Model) Close() {
	m.open = false
	m.query = ""
	m.cursor = 0
	m.caseSensitive = false
	m.matchCount = 0
	m.matchIndex = -1
}

// IsOpen reports whether the search overlay is visible.
func (m *Model) IsOpen() bool {
	return m.open
}

// Query returns the current search query.
func (m *Model) Query() string {
	return m.query
}

// CaseSensitive returns whether search matching is case-sensitive.
func (m *Model) CaseSensitive() bool {
	return m.caseSensitive
}

// SetSize sets the overlay width.
func (m *Model) SetSize(width int) {
	m.width = width
}

// MatchCount returns the number of matches being displayed.
func (m *Model) MatchCount() int {
	return m.matchCount
}

// MatchIndex returns the current match index, or -1 if there are no matches.
func (m *Model) MatchIndex() int {
	return m.matchIndex
}

// SetMatches updates the displayed match counter.
func (m *Model) SetMatches(count, index int) {
	m.matchCount = count
	if count == 0 {
		m.matchIndex = -1
		return
	}
	m.matchIndex = index
}

// ToggleCaseSensitive flips the case-sensitivity flag.
func (m *Model) ToggleCaseSensitive() {
	m.caseSensitive = !m.caseSensitive
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
	if !m.open {
		return nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	switch keyMsg.String() {
	case "esc":
		m.open = false
		m.query = ""
		m.cursor = 0
		m.caseSensitive = false
		return func() tea.Msg { return CloseMsg{} }
	case "enter", "ctrl+n", "down":
		return func() tea.Msg { return NextMsg{} }
	case "shift+enter", "ctrl+p", "up":
		return func() tea.Msg { return PreviousMsg{} }
	case "ctrl+o":
		m.caseSensitive = !m.caseSensitive
		return func() tea.Msg {
			return QueryChangedMsg{Query: m.query, CaseSensitive: m.caseSensitive}
		}
	case "backspace", "ctrl+h":
		m.deleteRuneBackward()
		return func() tea.Msg {
			return QueryChangedMsg{Query: m.query, CaseSensitive: m.caseSensitive}
		}
	case "left":
		if m.cursor > 0 {
			m.cursor--
		}
		return nil
	case "right":
		runes := []rune(m.query)
		if m.cursor < len(runes) {
			m.cursor++
		}
		return nil
	case "ctrl+u":
		m.query = ""
		m.cursor = 0
		return func() tea.Msg {
			return QueryChangedMsg{Query: m.query, CaseSensitive: m.caseSensitive}
		}
	case "ctrl+w":
		m.deleteWordBackward()
		return func() tea.Msg {
			return QueryChangedMsg{Query: m.query, CaseSensitive: m.caseSensitive}
		}
	}

	if text := appendableKeyText(keyMsg); text != "" {
		m.insert(text)
		return func() tea.Msg {
			return QueryChangedMsg{Query: m.query, CaseSensitive: m.caseSensitive}
		}
	}

	return nil
}

// View renders the search overlay line.
func (m *Model) View() tea.View {
	if !m.open {
		return tea.NewView("")
	}

	runes := []rune(m.query)
	before := string(runes[:m.cursor])
	after := string(runes[m.cursor:])

	caseHint := "[i]"
	if m.caseSensitive {
		caseHint = "[I]"
	}

	var counter string
	switch {
	case m.query == "":
		counter = ""
	case m.matchCount == 0:
		counter = "no matches"
	default:
		counter = fmt.Sprintf("%d/%d", m.matchIndex+1, m.matchCount)
	}

	query := before + "▐" + after
	base := caseHint + " find: " + query
	if counter != "" {
		base += " " + counter
	}

	// Account for border and padding so the overlay fits within the terminal.
	innerWidth := m.width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	if ansi.StringWidth(base) > innerWidth {
		base = ansi.Cut(base, 0, innerWidth)
	}

	return tea.NewView(m.styles.SearchOverlay.Render(base))
}

func (m *Model) insert(text string) {
	runes := []rune(m.query)
	cursor := m.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	insert := []rune(text)
	out := make([]rune, 0, len(runes)+len(insert))
	out = append(out, runes[:cursor]...)
	out = append(out, insert...)
	out = append(out, runes[cursor:]...)
	m.query = string(out)
	m.cursor = cursor + len(insert)
}

func (m *Model) deleteRuneBackward() {
	runes := []rune(m.query)
	cursor := m.cursor
	if cursor <= 0 || cursor > len(runes) {
		return
	}
	out := append(runes[:cursor-1], runes[cursor:]...)
	m.query = string(out)
	m.cursor = cursor - 1
}

func (m *Model) deleteWordBackward() {
	runes := []rune(m.query)
	cursor := m.cursor
	if cursor <= 0 || cursor > len(runes) {
		return
	}
	end := cursor
	for end > 0 && unicode.IsSpace(runes[end-1]) {
		end--
	}
	start := end
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	out := append(runes[:start], runes[end:]...)
	if start == 0 {
		trim := 0
		for trim < len(out) && unicode.IsSpace(out[trim]) {
			trim++
		}
		out = out[trim:]
		start = 0
	}
	m.query = string(out)
	m.cursor = start
}

func appendableKeyText(msg tea.KeyPressMsg) string {
	if msg.Text == "" {
		return ""
	}
	for _, r := range msg.Text {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return msg.Text
}
