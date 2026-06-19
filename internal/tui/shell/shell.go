// Package shell provides the quick shell overlay for the kimi-lite TUI.
package shell

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

const (
	prompt     = "shell$ "
	maxHistory = 50
)

// ExecuteMsg is emitted when the user presses Enter on a shell command.
// The root model must apply approval-mode checks before running it.
type ExecuteMsg struct {
	Command string
}

// ConfirmedExecuteMsg is emitted when the user confirms a shell command that
// required manual approval.
type ConfirmedExecuteMsg struct {
	Command string
}

// CancelMsg is emitted when the user cancels a running shell command.
type CancelMsg struct{}

// Model is the quick shell overlay component.
type Model struct {
	styles *styles.Styles

	open           bool
	input          string
	cursor         int // cursor position in runes
	running        bool
	command        string // currently running command
	confirming     bool   // waiting for manual approval confirmation
	confirmCommand string // command awaiting manual approval

	history    []string
	historyIdx int    // -1 means not navigating history
	draft      string // saved draft while navigating history

	width int
}

// New creates a new shell overlay model.
func New(st *styles.Styles) *Model {
	return &Model{
		styles:     st,
		historyIdx: -1,
	}
}

// Open shows the shell overlay.
func (m *Model) Open() {
	m.open = true
	m.input = ""
	m.cursor = 0
	m.historyIdx = -1
	m.draft = ""
	m.confirming = false
	m.confirmCommand = ""
}

// Close hides the shell overlay and resets draft state.
func (m *Model) Close() {
	m.open = false
	m.input = ""
	m.cursor = 0
	m.historyIdx = -1
	m.draft = ""
	m.confirming = false
	m.confirmCommand = ""
}

// Toggle toggles the shell overlay and reports whether it is now open.
func (m *Model) Toggle() bool {
	if m.open {
		m.Close()
		return false
	}
	m.Open()
	return true
}

// IsOpen reports whether the shell overlay is visible.
func (m *Model) IsOpen() bool {
	return m.open
}

// IsRunning reports whether a command is currently executing.
func (m *Model) IsRunning() bool {
	return m.running
}

// SetRunning updates the running state and the command being run.
func (m *Model) SetRunning(command string, running bool) {
	m.running = running
	if running {
		m.command = command
	} else {
		m.command = ""
	}
}

// RunningCommand returns the command currently executing, or an empty string.
func (m *Model) RunningCommand() string {
	return m.command
}

// IsConfirming reports whether the overlay is waiting for manual approval.
func (m *Model) IsConfirming() bool {
	return m.confirming
}

// SetConfirming sets the manual-approval confirmation state.
func (m *Model) SetConfirming(v bool) {
	m.confirming = v
	if !v {
		m.confirmCommand = ""
	}
}

// StartConfirmation enters the manual-approval confirmation state for command.
func (m *Model) StartConfirmation(command string) {
	m.confirming = true
	m.confirmCommand = command
}

// SetSize sets the overlay width.
func (m *Model) SetSize(width int) {
	m.width = width
}

// Input returns the current input value.
func (m *Model) Input() string {
	return m.input
}

// History returns a copy of the command history.
func (m *Model) History() []string {
	out := make([]string, len(m.history))
	copy(out, m.history)
	return out
}

// AddHistory appends a command to the history, avoiding consecutive duplicates.
func (m *Model) AddHistory(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == command {
		return
	}
	m.history = append(m.history, command)
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
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
	if !m.open {
		return nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	switch keyMsg.String() {
	case "esc":
		if m.confirming {
			m.confirming = false
			m.confirmCommand = ""
			return nil
		}
		if m.running {
			m.Close()
			return func() tea.Msg { return CancelMsg{} }
		}
		m.Close()
		return nil
	case "enter":
		if m.running {
			return nil
		}
		if m.confirming {
			command := m.confirmCommand
			m.confirming = false
			m.confirmCommand = ""
			m.running = true
			m.command = command
			m.input = ""
			m.cursor = 0
			m.historyIdx = -1
			m.draft = ""
			m.AddHistory(command)
			return func() tea.Msg { return ConfirmedExecuteMsg{Command: command} }
		}
		command := strings.TrimSpace(m.input)
		if command == "" {
			return nil
		}
		m.AddHistory(command)
		m.input = ""
		m.cursor = 0
		m.historyIdx = -1
		m.draft = ""
		return func() tea.Msg { return ExecuteMsg{Command: command} }
	case "ctrl+c":
		if m.running {
			return func() tea.Msg { return CancelMsg{} }
		}
		m.Close()
		return nil
	case "up":
		m.historyPrev()
		return nil
	case "down":
		m.historyNext()
		return nil
	case "left":
		if m.cursor > 0 {
			m.cursor--
		}
		return nil
	case "right":
		runes := []rune(m.input)
		if m.cursor < len(runes) {
			m.cursor++
		}
		return nil
	case "home":
		m.cursor = 0
		return nil
	case "end":
		m.cursor = len([]rune(m.input))
		return nil
	case "backspace", "ctrl+h":
		m.deleteRuneBackward()
		return nil
	case "ctrl+u":
		m.input = ""
		m.cursor = 0
		m.historyIdx = -1
		m.draft = ""
		return nil
	case "ctrl+w":
		m.deleteWordBackward()
		return nil
	}

	if text := appendableKeyText(keyMsg); text != "" {
		m.insert(text)
	}

	return nil
}

// View renders the shell overlay line.
func (m *Model) View() tea.View {
	if !m.open {
		return tea.NewView("")
	}

	runes := []rune(m.input)
	cursor := m.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}

	var line string
	if m.confirming {
		line = "Run " + shellQuote(m.confirmCommand) + "? [Enter] yes [Esc] no"
	} else {
		before := string(runes[:cursor])
		after := string(runes[cursor:])
		line = m.styles.ShellPrompt.Render(prompt) + before + "▌" + after
	}

	innerWidth := m.width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	if ansi.StringWidth(line) > innerWidth {
		line = ansi.Cut(line, ansi.StringWidth(line)-innerWidth, ansi.StringWidth(line))
	}

	return tea.NewView(m.styles.ShellOverlay.Render(line))
}

// Height returns the rendered height of the overlay.
func (m *Model) Height() int {
	if !m.open {
		return 0
	}
	return lipgloss.Height(m.View().Content)
}

func (m *Model) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.draft = m.input
		m.historyIdx = len(m.history) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	} else {
		return
	}
	m.input = m.history[m.historyIdx]
	m.cursor = len([]rune(m.input))
}

func (m *Model) historyNext() {
	if m.historyIdx == -1 {
		return
	}
	m.historyIdx++
	if m.historyIdx >= len(m.history) {
		m.input = m.draft
		m.historyIdx = -1
	} else {
		m.input = m.history[m.historyIdx]
	}
	m.cursor = len([]rune(m.input))
}

func (m *Model) insert(text string) {
	runes := []rune(m.input)
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
	m.input = string(out)
	m.cursor = cursor + len(insert)
}

func (m *Model) deleteRuneBackward() {
	runes := []rune(m.input)
	cursor := m.cursor
	if cursor <= 0 || cursor > len(runes) {
		return
	}
	out := append(runes[:cursor-1], runes[cursor:]...)
	m.input = string(out)
	m.cursor = cursor - 1
}

func (m *Model) deleteWordBackward() {
	runes := []rune(m.input)
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
	m.input = string(out)
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

// shellQuote returns s unchanged if it contains no shell-special characters;
// otherwise it returns the string wrapped in single quotes with embedded
// single quotes escaped for POSIX shells.
func shellQuote(s string) string {
	if strings.ContainsAny(s, " \"'`)&|;<>{}[]*?#!$\\") {
		return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
	}
	return s
}
