// Package input provides a multi-line input component for the kimi-lite TUI.
package input

import (
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const inputWidthPadding = 4

// SendMsg is emitted when the user wants to send the current input.
type SendMsg struct {
	Content string
}

// KeyMap defines keybindings for the input component.
type KeyMap struct {
	Send    key.Binding
	Newline key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Newline: key.NewBinding(
			key.WithKeys("alt+enter"),
			key.WithHelp("alt+enter", "newline"),
		),
	}
}

// ConfigurableKeyMap returns a KeyMap from api.KeybindingConfig.
func ConfigurableKeyMap(cfg api.KeybindingConfig) KeyMap {
	km := DefaultKeyMap()
	if cfg.Send != "" {
		km.Send = key.NewBinding(key.WithKeys(cfg.Send))
	}
	if cfg.Newline != "" {
		km.Newline = key.NewBinding(key.WithKeys(cfg.Newline))
	}
	return km
}

// Model is the input component model.
type Model struct {
	textarea textarea.Model
	styles   *styles.Styles
	keyMap   KeyMap
	history  []string
	histIdx  int // -1 means current draft, >=0 means history index
	draft    string
	width    int
	mu       sync.RWMutex
}

// New creates a new input model.
func New(st *styles.Styles, keyMap KeyMap) *Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send, Alt+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.Focus()

	m := &Model{
		textarea: ta,
		styles:   st,
		keyMap:   keyMap,
		history:  make([]string, 0),
		histIdx:  -1,
	}
	m.updateStyles()
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		m.mu.RLock()
		km := m.keyMap
		m.mu.RUnlock()

		if key.Matches(msg, km.Send) {
			content := strings.TrimSpace(m.textarea.Value())
			if content != "" {
				m.mu.Lock()
				m.history = append(m.history, content)
				m.histIdx = -1
				m.draft = ""
				m.mu.Unlock()
				m.textarea.Reset()
				return m, func() tea.Msg {
					return SendMsg{Content: content}
				}
			}
			return m, nil
		}

		if key.Matches(msg, km.Newline) {
			m.textarea.InsertString("\n")
			return m, nil
		}

		// History navigation
		if msg.String() == "up" || msg.String() == "ctrl+p" {
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.history) == 0 {
				return m, nil
			}
			if m.histIdx == -1 {
				m.draft = m.textarea.Value()
				m.histIdx = len(m.history) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.textarea.SetValue(m.history[m.histIdx])
			m.textarea.CursorEnd()
			return m, tea.Batch(cmds...)
		}

		if msg.String() == "down" || msg.String() == "ctrl+n" {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.histIdx == -1 {
				return m, nil
			}
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.textarea.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = -1
				m.textarea.SetValue(m.draft)
			}
			m.textarea.CursorEnd()
			return m, tea.Batch(cmds...)
		}
	}

	// Pass other messages to textarea
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() string {
	return m.styles.InputBox.Render(m.textarea.View())
}

// Height returns the rendered height of the input component.
func (m *Model) Height() int {
	return lipgloss.Height(m.View())
}

// SetWidth sets the component width.
func (m *Model) SetWidth(w int) {
	m.width = w
	m.textarea.SetWidth(w - inputWidthPadding) // account for padding/border
}

// Focus focuses the input.
func (m *Model) Focus() tea.Cmd {
	return m.textarea.Focus()
}

// Blur blurs the input.
func (m *Model) Blur() {
	m.textarea.Blur()
}

// Value returns the current input value.
func (m *Model) Value() string {
	return m.textarea.Value()
}

// Reset clears the input.
func (m *Model) Reset() {
	m.textarea.Reset()
}

// SetValue sets the input value.
func (m *Model) SetValue(s string) {
	m.textarea.SetValue(s)
}

func (m *Model) updateStyles() {
	if m.styles == nil {
		return
	}
	m.textarea.FocusedStyle.CursorLine = lipgloss.NewStyle()
	m.textarea.FocusedStyle.Base = lipgloss.NewStyle().
		Background(m.styles.Theme.InputBg).
		Foreground(m.styles.Theme.Foreground)
	m.textarea.BlurredStyle.Base = lipgloss.NewStyle().
		Background(m.styles.Theme.InputBg).
		Foreground(m.styles.Theme.Muted)
}
