// Package messages provides chat message rendering for the kimi-lite TUI.
package messages

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	messageWidthPadding = 8
	minMessageWidth     = 20
	renderDebounce      = 200 * time.Millisecond
)

// Type represents the kind of message.
type Type int

const (
	TypeUser Type = iota
	TypeAssistant
	TypeToolCall
	TypeError
)

// KeyMap defines keybindings for the message component.
type KeyMap struct {
	ToggleExpand key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		ToggleExpand: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "toggle expand"),
		),
	}
}

// Message represents a single chat message as a Bubble Tea model.
type Message struct {
	Type    Type
	Content string
	Role    api.Role

	// ToolCall fields (only for TypeToolCall)
	ToolCall   api.ToolCall
	ToolResult *api.ToolResult

	// Error fields (only for TypeError)
	Err error

	// Assistant rendering
	RawMode     bool   // if true, show raw markdown instead of rendered
	Rendered    string // cached glamour output
	renderCache string // content that was rendered

	// Debounce state
	needsRender bool
	lastRender  time.Time
	Streaming   bool // true while content is being streamed

	// State
	Expanded bool
	Width    int
	Styles   *styles.Styles
	KeyMap   KeyMap
}

// NewUserMessage creates a new user message.
func NewUserMessage(content string, st *styles.Styles) *Message {
	return &Message{
		Type:    TypeUser,
		Content: content,
		Role:    api.RoleUser,
		Styles:  st,
		KeyMap:  DefaultKeyMap(),
	}
}

// NewAssistantMessage creates a new assistant message.
func NewAssistantMessage(content string, st *styles.Styles) *Message {
	return &Message{
		Type:    TypeAssistant,
		Content: content,
		Role:    api.RoleAssistant,
		Styles:  st,
		KeyMap:  DefaultKeyMap(),
	}
}

// NewToolCallMessage creates a new tool call display message.
func NewToolCallMessage(call api.ToolCall, st *styles.Styles) *Message {
	return &Message{
		Type:     TypeToolCall,
		ToolCall: call,
		Role:     api.RoleTool,
		Styles:   st,
		KeyMap:   DefaultKeyMap(),
	}
}

// NewErrorMessage creates a new error display message.
func NewErrorMessage(err error, st *styles.Styles) *Message {
	return &Message{
		Type:    TypeError,
		Content: err.Error(),
		Err:     err,
		Role:    api.RoleSystem,
		Styles:  st,
		KeyMap:  DefaultKeyMap(),
	}
}

// Init implements tea.Model.
func (m *Message) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Message) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, m.KeyMap.ToggleExpand) && m.Type == TypeToolCall {
			m.Expanded = !m.Expanded
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft && m.Type == TypeToolCall {
			m.Expanded = !m.Expanded
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *Message) View() string {
	switch m.Type {
	case TypeUser:
		return m.viewUser()
	case TypeAssistant:
		return m.viewAssistant()
	case TypeToolCall:
		return m.viewToolCall()
	case TypeError:
		return m.viewError()
	default:
		return ""
	}
}

// SetWidth sets the rendering width.
func (m *Message) SetWidth(w int) {
	m.Width = w
}

// AppendContent appends content to the message (for streaming).
func (m *Message) AppendContent(s string) {
	if m.Type != TypeAssistant {
		return
	}
	m.Content += s
	m.needsRender = true
}

// SetStreaming marks whether the message is currently being streamed.
func (m *Message) SetStreaming(streaming bool) {
	m.Streaming = streaming
	if !m.Streaming {
		m.needsRender = true
	}
}

// SetToolResult sets the result for a tool call message.
func (m *Message) SetToolResult(r api.ToolResult) {
	m.ToolResult = &r
}

func (m *Message) viewUser() string {
	prefix := m.Styles.UserMessage.Render("You")
	content := wordWrap(m.Content, max(m.Width-messageWidthPadding, minMessageWidth))
	body := m.Styles.UserMessage.Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, prefix, body)
}

func (m *Message) viewAssistant() string {
	prefix := m.Styles.AssistantMessage.Render("Assistant")
	var body string
	if m.RawMode {
		body = m.Styles.AssistantMessage.Render(wordWrap(m.Content, max(m.Width-messageWidthPadding, minMessageWidth)))
	} else {
		body = m.renderedContent()
	}
	return lipgloss.JoinVertical(lipgloss.Left, prefix, body)
}

func (m *Message) renderedContent() string {
	if !m.Streaming && m.Rendered != "" && m.renderCache == m.Content && !m.needsRender {
		return m.Rendered
	}

	shouldRender := false
	if !m.Streaming {
		// Streaming is done, always render
		shouldRender = true
	} else if m.needsRender && time.Since(m.lastRender) >= renderDebounce {
		// Debounce: only render if 200ms passed since last render
		shouldRender = true
	}

	if shouldRender {
		rendered := safeGlamourRender(m.Content, m.Styles.Theme.Name)
		m.Rendered = rendered
		m.renderCache = m.Content
		m.needsRender = false
		m.lastRender = time.Now()
	}

	// During active streaming, show raw text
	if m.Streaming {
		return m.Content
	}

	if m.Rendered != "" {
		return m.Rendered
	}
	return m.Content
}

func (m *Message) viewToolCall() string {
	var b strings.Builder
	icon := "▸"
	if m.Expanded {
		icon = "▾"
	}
	status := "pending"
	if m.ToolResult != nil {
		if m.ToolResult.Error != "" {
			status = "error"
		} else {
			status = "done"
		}
	}
	header := fmt.Sprintf("%s Tool: %s (%s)", icon, m.ToolCall.Name, status)
	b.WriteString(m.Styles.ToolCall.Render(header))

	if m.Expanded {
		b.WriteString("\n")
		args := wordWrap(m.ToolCall.Arguments, max(m.Width-messageWidthPadding, minMessageWidth))
		b.WriteString(m.Styles.ToolCallExpanded.Render("Arguments: " + args))
		if m.ToolResult != nil {
			b.WriteString("\n")
			out := wordWrap(m.ToolResult.Output, max(m.Width-messageWidthPadding, minMessageWidth))
			if m.ToolResult.Error != "" {
				out = wordWrap(m.ToolResult.Error, max(m.Width-messageWidthPadding, minMessageWidth))
				b.WriteString(m.Styles.ErrorMessage.Render("Error: " + out))
			} else {
				b.WriteString(m.Styles.ToolCallExpanded.Render("Output: " + out))
			}
		}
	}
	return b.String()
}

func (m *Message) viewError() string {
	prefix := m.Styles.ErrorMessage.Render("Error")
	content := wordWrap(m.Content, max(m.Width-messageWidthPadding, minMessageWidth))
	body := m.Styles.ErrorMessage.Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, prefix, body)
}

func safeGlamourRender(content, theme string) (rendered string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("glamour.Render panicked", "recover", r)
			rendered = content
		}
	}()
	var err error
	rendered, err = glamour.Render(content, theme)
	if err != nil {
		rendered = content
	}
	return
}

func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		runes := []rune(line)
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return strings.Join(out, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
