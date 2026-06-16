// Package footer provides the two-line status footer for the kimi-lite TUI.
package footer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const refreshInterval = 5 * time.Second

// Mode constants mirror the root model approval-mode values.
const (
	ModeAuto = 1
	ModeYolo = 2
)

// Data is the snapshot of information the footer needs each frame.
type Data struct {
	ModelName   string
	Mode        int // approval mode (ModeAuto / ModeYolo)
	State       api.TurnState
	StatusText  string
	CWD         string
	ContextUsed int
	ContextMax  int
	ToolCount   int
	GitBranch   string
	GitDirty    bool
	GitAhead    int
	GitBehind   int
}

// Model is the footer component.
type Model struct {
	styles *styles.Styles
	width  int
	data   Data
	tipIdx int
}

// New creates a footer model.
func New(st *styles.Styles) *Model {
	return &Model{styles: st}
}

// SetSize sets the footer width.
func (m *Model) SetSize(w int) {
	m.width = w
}

// SetData updates the footer data.
func (m *Model) SetData(d Data) {
	m.data = d
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return m.tickCmd()
}

// UpdateMsg processes messages.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case tickMsg:
		m.tipIdx++
		return m.tickCmd()
	}
	return nil
}

type tickMsg struct{}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// View renders the footer as a two-line string.
func (m *Model) View() string {
	if m.width <= 0 {
		return ""
	}
	line1 := m.line1()
	line2 := m.line2()
	return lipgloss.JoinVertical(lipgloss.Left, line1, line2)
}

func (m *Model) line1() string {
	var parts []string
	mode := m.modeBadge()
	if mode != "" {
		parts = append(parts, mode)
	}
	parts = append(parts, m.styles.FooterModel.Render(" "+m.data.ModelName+" "))

	cwd := m.shortCWD()
	if cwd != "" {
		parts = append(parts, m.styles.FooterCWD.Render(" "+cwd+" "))
	}

	git := m.gitBadge()
	if git != "" {
		parts = append(parts, m.styles.FooterGit.Render(" "+git+" "))
	}

	left := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	tip := m.styles.FooterTip.Render(m.rotatingTip())
	padding := m.width - ansi.StringWidth(left) - ansi.StringWidth(tip)
	if padding < 0 {
		padding = 0
	}
	return left + strings.Repeat(" ", padding) + tip
}

func (m *Model) line2() string {
	left := m.statusPart()
	right := m.contextPart()
	padding := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if padding < 0 {
		padding = 0
	}
	return left + strings.Repeat(" ", padding) + right
}

func (m *Model) modeBadge() string {
	switch m.data.Mode {
	case ModeYolo:
		return m.styles.ModeBadgeYolo.Render(" YOLO ")
	case ModeAuto:
		return m.styles.ModeBadgeAuto.Render(" AUTO ")
	default:
		return ""
	}
}

func (m *Model) shortCWD() string {
	cwd := m.data.CWD
	if cwd == "" {
		cwd, _ = filepath.Abs(".")
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}
	parts := strings.Split(cwd, string(filepath.Separator))
	if len(parts) > 3 {
		parts = append([]string{"..."}, parts[len(parts)-3:]...)
	}
	return filepath.Join(parts...)
}

func (m *Model) gitBadge() string {
	if m.data.GitBranch == "" {
		return ""
	}
	badge := m.data.GitBranch
	if m.data.GitDirty {
		badge += "*"
	}
	if m.data.GitAhead > 0 || m.data.GitBehind > 0 {
		badge += fmt.Sprintf(" %d/%d", m.data.GitAhead, m.data.GitBehind)
	}
	return badge
}

func (m *Model) rotatingTip() string {
	tips := []string{
		"enter: send message",
		"alt+enter: insert newline",
		"enter: expand tool output",
		"r: toggle raw markdown",
		"tab/shift+tab: switch focus",
		"ctrl+g: external editor",
		"ctrl+y: toggle yolo mode",
		"@: mention files",
		"/compact: compact context",
		"/sessions: switch session",
	}
	if m.tipIdx < 0 {
		m.tipIdx = 0
	}
	return tips[m.tipIdx%len(tips)]
}

func (m *Model) statusPart() string {
	if m.data.StatusText != "" {
		return m.styles.FooterStatus.Render(" " + truncate(m.data.StatusText, m.width/2) + " ")
	}
	return m.styles.FooterStatus.Render(" " + m.data.State.ShortString() + " ")
}

func (m *Model) contextPart() string {
	if m.data.ContextMax > 0 {
		pct := float64(m.data.ContextUsed) / float64(m.data.ContextMax) * 100
		return m.styles.FooterContext.Render(fmt.Sprintf(" context: %.1f%% ", pct))
	}
	return ""
}

func truncate(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Cut(s, 0, w-1) + "…"
}
