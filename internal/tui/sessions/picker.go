// Package sessions provides a TUI picker for browsing and selecting sessions.
package sessions

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

	style pickerStyle
}

type pickerStyle struct {
	border      lipgloss.Style
	title       lipgloss.Style
	item        lipgloss.Style
	selected    lipgloss.Style
	header      lipgloss.Style
	footer      lipgloss.Style
	placeholder lipgloss.Style
}

func defaultPickerStyle() pickerStyle {
	return pickerStyle{
		border:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2),
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")),
		item:        lipgloss.NewStyle(),
		selected:    lipgloss.NewStyle().Foreground(lipgloss.Color("#212121")).Background(lipgloss.Color("#FDD835")),
		header:      lipgloss.NewStyle().Foreground(lipgloss.Color("#A3A3A3")),
		footer:      lipgloss.NewStyle().Foreground(lipgloss.Color("#A3A3A3")),
		placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("#737373")),
	}
}

// NewPicker creates a picker for the provided sessions.
func NewPicker(sessions []api.Session, currentPath string, width, height int) *Picker {
	p := &Picker{
		sessions: sessions,
		filtered: sessions,
		pageSize: 10,
		width:    width,
		height:   height,
		path:     currentPath,
		style:    defaultPickerStyle(),
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
// be closed, and selected=true when the user chose a session.
func (p *Picker) Update(msg tea.KeyPressMsg) (done, selected bool) {
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
			return true, true
		}
		return true, false
	case tea.KeyEscape:
		if p.searching {
			p.searching = false
			return false, false
		}
		return true, false
	case tea.KeyBackspace:
		if p.searching && len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.filter()
		}
	default:
		if msg.Mod == tea.ModCtrl && msg.Code == 'c' {
			if p.searching {
				p.searching = false
				return false, false
			}
			return true, false
		}
		text := msg.Text
		if text == "" {
			return false, false
		}
		if !p.searching && text == "/" {
			p.searching = true
			p.query = ""
			p.filter()
			return false, false
		}
		if !p.searching && strings.EqualFold(text, "a") {
			p.ToggleAll()
			return false, false
		}
		if p.searching {
			p.query += text
			p.filter()
		}
	}
	return false, false
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
	b.WriteString(p.style.title.Render("Sessions") + "\n")
	mode := "current directory"
	if p.allMode {
		mode = "all directories"
	}
	searchLine := fmt.Sprintf("Mode: %s | %d sessions", mode, len(p.filtered))
	if p.searching {
		searchLine = fmt.Sprintf("Search: %s_ | %d matches", p.query, len(p.filtered))
	}
	b.WriteString(p.style.header.Render(searchLine) + "\n\n")

	if len(p.filtered) == 0 {
		placeholder := "No sessions match."
		if !p.allMode && p.path != "" && p.query == "" {
			placeholder = "No sessions in this directory. Press 'a' to show all sessions."
		}
		b.WriteString(p.style.placeholder.Render(placeholder))
	} else {
		start, end := p.visibleRange()
		for i := start; i < end && i < len(p.filtered); i++ {
			s := p.filtered[i]
			line := p.formatItem(s, innerW, i == p.cursor)
			b.WriteString(line + "\n")
		}
	}

	pos := p.cursor + 1
	if len(p.filtered) == 0 {
		pos = 0
	}
	footer := fmt.Sprintf("↑/↓ move • PgUp/PgDown page • Enter select • Esc close • / search • a toggle all (%d/%d)", pos, len(p.filtered))
	b.WriteString("\n" + p.style.footer.Render(footer))

	return p.style.border.Render(b.String())
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
	// Reserve header + footer + borders.
	available := p.height - 8
	if available < 3 {
		available = 3
	}
	return available
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

func (p *Picker) formatItem(s api.Session, width int, selected bool) string {
	label := s.ID
	if s.Name != "" {
		label = fmt.Sprintf("%s (%s)", s.Name, s.ID)
	}
	line := fmt.Sprintf("%s — %s — %s", label, s.Path, s.UpdatedAt.Format("2006-01-02 15:04"))
	if len(line) > width {
		line = line[:width]
	}
	if selected {
		return p.style.selected.Render("> " + line)
	}
	return p.style.item.Render("  " + line)
}
