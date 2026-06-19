// Package sessions provides a TUI picker for browsing and selecting sessions.
package sessions

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// Picker is a modal session-selection list with search and pagination.
type Picker struct {
	sessions  []api.Session
	filtered  []api.Session
	cursor    int
	pageSize  int
	width     int
	height    int
	query     string
	searching bool
	allMode   bool
	path      string

	styles *styles.Styles
}

// NewPicker creates a picker for the provided sessions.
func NewPicker(sessions []api.Session, currentPath string, width, height int, st *styles.Styles) *Picker {
	p := &Picker{
		sessions: sessions,
		filtered: sessions,
		pageSize: 10,
		width:    width,
		height:   height,
		path:     currentPath,
		styles:   st,
	}
	p.filter()
	p.pageSize = p.computePageSize()
	return p
}

// SetSessions replaces the session list and re-applies the current filter.
func (p *Picker) SetSessions(sessions []api.Session) {
	p.sessions = sessions
	p.filter()
}

// SetSize updates the picker dimensions and recalculates the page size.
func (p *Picker) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.pageSize = p.computePageSize()
}

// ToggleAll switches between all sessions and sessions matching the initial path.
func (p *Picker) ToggleAll() {
	p.allMode = !p.allMode
	p.filter()
}

// AllMode reports whether the picker is showing sessions across all paths.
func (p *Picker) AllMode() bool { return p.allMode }

// Selected returns the currently highlighted session. The caller should check
// that a selection exists using HasSelection.
func (p *Picker) Selected() api.Session {
	if len(p.filtered) == 0 {
		return api.Session{}
	}
	return p.filtered[p.cursor]
}

// HasSelection reports whether there is a session to select.
func (p *Picker) HasSelection() bool {
	return len(p.filtered) > 0
}

// Update handles keyboard input. It returns done=true when the picker should
// be closed, selected=true when the user chose a session, and copyCmd=true
// when the caller should copy the current resume command to the clipboard.
func (p *Picker) Update(msg tea.KeyPressMsg) (done, selected, copyCmd bool) {
	switch msg.Code {
	case tea.KeyUp:
		p.move(-1)
	case tea.KeyDown:
		p.move(1)
	case tea.KeyPgUp:
		p.page(-1)
	case tea.KeyPgDown:
		p.page(1)
	case tea.KeyHome:
		p.cursor = 0
	case tea.KeyEnd:
		p.cursor = len(p.filtered) - 1
		if p.cursor < 0 {
			p.cursor = 0
		}
	case tea.KeyEnter:
		if p.HasSelection() {
			return true, true, false
		}
		return true, false, false
	case tea.KeyEscape:
		if p.searching {
			p.searching = false
			p.query = ""
			p.filter()
			return false, false, false
		}
		return true, false, false
	case tea.KeyBackspace:
		if p.searching && len(p.query) > 0 {
			runes := []rune(p.query)
			p.query = string(runes[:len(runes)-1])
			p.filter()
		}
	default:
		if msg.Mod == tea.ModCtrl && msg.Code == 'c' {
			if p.searching {
				p.searching = false
				return false, false, false
			}
			return true, false, false
		}
		text := msg.Text
		if text == "" {
			return false, false, false
		}
		if !p.searching && text == "/" {
			p.searching = true
			p.query = ""
			p.filter()
			return false, false, false
		}
		if !p.searching && strings.EqualFold(text, "a") {
			p.ToggleAll()
			return false, false, false
		}
		if !p.searching && strings.EqualFold(text, "y") {
			return false, false, true
		}
		if p.searching {
			p.query += text
			p.filter()
		}
	}
	return false, false, false
}

// View renders the picker as a string suitable for overlaying on the TUI.
func (p *Picker) View() string {
	if p.width < 10 || p.height < 5 {
		return ""
	}

	innerW := p.width - 4
	if innerW < 10 {
		innerW = 10
	}

	var b strings.Builder
	b.WriteString(p.styles.PickerTitle.Render("Sessions") + "\n")
	mode := "current directory"
	if p.allMode {
		mode = "all directories"
	}
	countLabel := "sessions"
	if len(p.filtered) == 1 {
		countLabel = "session"
	}
	searchLine := fmt.Sprintf("Mode: %s | %d %s", mode, len(p.filtered), countLabel)
	if p.searching {
		searchLine = fmt.Sprintf("Search: %s_ | %d matches", p.query, len(p.filtered))
	}
	searchLine = truncateWidth(searchLine, innerW)
	b.WriteString(p.styles.PickerHeader.Render(searchLine) + "\n\n")

	if len(p.filtered) == 0 {
		placeholder := "No sessions match."
		if !p.allMode && p.path != "" && p.query == "" {
			placeholder = "No sessions in this directory. Press 'a' to show all sessions."
		}
		placeholder = truncateWidth(placeholder, innerW)
		b.WriteString(p.styles.PickerPlaceholder.Render(placeholder))
	} else {
		start, end := p.visibleRange()
		for i := start; i < end && i < len(p.filtered); i++ {
			s := p.filtered[i]
			card := p.formatCard(s, innerW, i == p.cursor)
			b.WriteString(card + "\n")
		}
	}

	pos := p.cursor + 1
	if len(p.filtered) == 0 {
		pos = 0
	}
	footer := fmt.Sprintf("↑/↓ move • PgUp/PgDown page • Enter select • Esc close • / search • a toggle all • y copy (%d/%d)", pos, len(p.filtered))
	footer = truncateWidth(footer, innerW)
	b.WriteString("\n" + p.styles.PickerFooter.Render(footer))

	return p.styles.PickerBorder.Width(innerW).Render(b.String())
}

func (p *Picker) move(delta int) {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
}

func (p *Picker) page(delta int) {
	p.cursor += delta * p.pageSize
	if p.cursor < 0 {
		p.cursor = 0
	}
	if len(p.filtered) == 0 {
		p.cursor = 0
		return
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
}

func (p *Picker) visibleRange() (int, int) {
	if len(p.filtered) == 0 {
		return 0, 0
	}
	pageSize := p.pageSize
	if pageSize < 1 {
		pageSize = 10
	}
	page := p.cursor / pageSize
	start := page * pageSize
	end := start + pageSize
	if end > len(p.filtered) {
		end = len(p.filtered)
	}
	return start, end
}

func (p *Picker) computePageSize() int {
	// Reserve header + footer + borders; each card can span up to 4 lines.
	available := p.height - 8
	if available < 4 {
		available = 4
	}
	return available / 4
}

func (p *Picker) filter() {
	var out []api.Session
	q := strings.ToLower(strings.TrimSpace(p.query))
	for _, s := range p.sessions {
		if !p.allMode && p.path != "" && s.Path != p.path {
			continue
		}
		if q == "" {
			out = append(out, s)
			continue
		}
		if strings.Contains(strings.ToLower(s.ID), q) ||
			strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Path), q) {
			out = append(out, s)
		}
	}
	p.filtered = out
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *Picker) formatCard(s api.Session, width int, selected bool) string {
	if width < 5 {
		width = 5
	}
	maxContent := width - 4

	var b strings.Builder
	marker := "  "
	if s.Path == p.path {
		marker = "← "
	}
	title := s.Name
	if title == "" {
		title = s.ID
	}
	top := fmt.Sprintf("%s%s • %s", marker, title, humanizeTime(s.UpdatedAt))
	top = truncateWidth(top, maxContent)
	b.WriteString(top + "\n")
	pathLine := fmt.Sprintf("   %s", s.Path)
	pathLine = truncateWidth(pathLine, maxContent)
	b.WriteString(pathLine + "\n")
	if s.LastPrompt != "" {
		prompt := truncateWidth(s.LastPrompt, maxContent)
		fmt.Fprintf(&b, "   %s\n", prompt)
	}
	card := strings.TrimSuffix(b.String(), "\n")
	if selected {
		return p.styles.PickerSelected.Render(card)
	}
	return p.styles.PickerItem.Render(card)
}

// truncateWidth truncates s to fit within maxWidth display cells, appending an
// ellipsis when truncated. Wide runes and ANSI sequences are handled via the
// charmbracelet/x/ansi package.
func truncateWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	return ansi.Cut(s, 0, maxWidth-1) + "…"
}

// humanizeTime returns a short relative timestamp such as "2m ago" or the
// absolute date for older timestamps.
func humanizeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}
