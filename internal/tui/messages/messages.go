// Package messages provides chat message rendering for the kimi-lite TUI.
package messages

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type cachedRenderer struct {
	r  *glamour.TermRenderer
	mu sync.Mutex
}

var rendererCache sync.Map // key: theme string, value: *cachedRenderer

const (
	messageWidthPadding = 8
	minMessageWidth     = 20
	streamingCursor     = "▍"
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
	ToggleExpand  key.Binding
	ToggleRawMode key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		ToggleExpand: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "toggle expand"),
		),
		ToggleRawMode: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "toggle raw markdown"),
		),
	}
}

// Message represents a single chat message as a Bubble Tea model.
type Message struct {
	Type    Type
	Content string
	Role    api.Role

	// ContentParts holds multi-modal parts (text, images) for user/assistant/tool
	// messages. They are rendered as placeholders or links in the transcript.
	ContentParts []api.ContentPart

	// ToolCall fields (only for TypeToolCall)
	ToolCall   api.ToolCall
	ToolResult *api.ToolResult

	// ToolCall timing tracks elapsed duration.
	ToolStart time.Time
	ToolEnd   time.Time

	// Error fields (only for TypeError)
	Err error

	// Assistant rendering
	Rendered         string // cached glamour output
	renderCache      string // content that was rendered
	renderCacheWidth int    // width used for the cached render
	RawMode          bool   // when true, bypass glamour and show raw markdown

	// Debounce state
	needsRender bool
	Streaming   bool // true while content is being streamed

	// State
	Expanded bool
	Width    int
	Styles   *styles.Styles
	KeyMap   KeyMap

	// Cached wrapped output for non-assistant messages
	cachedView    string
	cacheWidth    int
	cacheExpanded bool

	mu sync.RWMutex
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
		Type:      TypeToolCall,
		ToolCall:  call,
		Role:      api.RoleTool,
		Styles:    st,
		KeyMap:    DefaultKeyMap(),
		ToolStart: time.Now(),
	}
}

// NewErrorMessage creates a new error display message. A nil error is treated
// as an empty error message rather than panicking.
func NewErrorMessage(err error, st *styles.Styles) *Message {
	if err == nil {
		return &Message{
			Type:    TypeError,
			Content: "",
			Err:     nil,
			Role:    api.RoleSystem,
			Styles:  st,
			KeyMap:  DefaultKeyMap(),
		}
	}
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
	cmd := m.UpdateMsg(msg)
	return m, cmd
}

// UpdateMsg processes a message and returns the resulting command.
func (m *Message) UpdateMsg(msg tea.Msg) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, m.KeyMap.ToggleExpand) && m.Type == TypeToolCall {
			m.Expanded = !m.Expanded
			m.cacheWidth = -1
		}
		if key.Matches(msg, m.KeyMap.ToggleRawMode) && m.Type == TypeAssistant {
			m.toggleRawModeLocked()
			return func() tea.Msg { return RenderInvalidateMsg{} }
		}
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft && m.Type == TypeToolCall {
			m.Expanded = !m.Expanded
			m.cacheWidth = -1
		}
	}
	return nil
}

// View implements tea.Model.
func (m *Message) View() tea.View {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.Type {
	case TypeUser:
		return tea.NewView(m.viewUser())
	case TypeAssistant:
		return tea.NewView(m.viewAssistant())
	case TypeToolCall:
		return tea.NewView(m.viewToolCall())
	case TypeError:
		return tea.NewView(m.viewError())
	default:
		return tea.NewView("")
	}
}

// SetWidth sets the rendering width.
func (m *Message) SetWidth(w int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Width != w {
		m.cacheWidth = -1
	}
	m.Width = w
}

// AppendContent appends content to the message (for streaming).
func (m *Message) AppendContent(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Type != TypeAssistant {
		return
	}
	m.Content += s
	m.needsRender = true
}

// SetStreaming marks whether the message is currently being streamed.
func (m *Message) SetStreaming(streaming bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Streaming = streaming
	if !m.Streaming {
		m.needsRender = true
	}
}

func (m *Message) setRawModeLocked(raw bool) {
	if m.RawMode == raw {
		return
	}
	m.RawMode = raw
	m.Rendered = ""
	m.renderCache = ""
	m.renderCacheWidth = 0
	m.needsRender = true
}

func (m *Message) toggleRawModeLocked() {
	m.setRawModeLocked(!m.RawMode)
}

// SetRawMode toggles raw markdown rendering for assistant messages.
// It clears the render cache so the next View() reflects the new mode.
func (m *Message) SetRawMode(raw bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setRawModeLocked(raw)
}

// ToggleRawMode flips the current raw-mode state.
func (m *Message) ToggleRawMode() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggleRawModeLocked()
}

// RenderInvalidateMsg signals that the transcript render cache needs to be rebuilt.
type RenderInvalidateMsg struct{}

// SetToolResult sets the result for a tool call message.
func (m *Message) SetToolResult(r api.ToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ToolResult = &r
	m.ToolEnd = time.Now()
	m.cacheWidth = -1
}

func (m *Message) viewUser() string {
	if m.cacheWidth == m.Width && m.cachedView != "" {
		return m.cachedView
	}
	prefix := m.Styles.UserMessage.Render("You")
	width := max(m.Width-messageWidthPadding, minMessageWidth)
	content := contentWithParts(m.Content, m.ContentParts)
	content = wordWrap(content, width)
	body := m.Styles.UserMessage.Render(content)
	m.cachedView = lipgloss.JoinVertical(lipgloss.Left, prefix, body)
	m.cacheWidth = m.Width
	return m.cachedView
}

func (m *Message) viewAssistant() string {
	prefix := m.Styles.AssistantMessage.Render("Assistant")
	body := m.renderedContent()
	return lipgloss.JoinVertical(lipgloss.Left, prefix, body)
}

func (m *Message) renderedContent() string {
	width := max(m.Width-messageWidthPadding, minMessageWidth)
	source := contentWithParts(m.Content, m.ContentParts)

	if !m.Streaming && m.Rendered != "" && m.renderCache == source && m.renderCacheWidth == width && !m.needsRender {
		return m.Rendered
	}

	// Raw mode bypasses glamour for finished assistant messages.
	if m.RawMode && !m.Streaming {
		m.renderCache = source
		m.renderCacheWidth = width
		m.needsRender = false
		return wordWrap(source, width)
	}

	if !m.Streaming {
		rendered := safeGlamourRender(source, m.Styles.Theme.Name, width)
		m.Rendered = rendered
		m.renderCache = source
		m.renderCacheWidth = width
		m.needsRender = false
	}

	// During active streaming, show raw text with a cursor indicator.
	if m.Streaming {
		wrapped := wordWrap(source, width)
		if wrapped == "" {
			return streamingCursor
		}
		return wrapped + streamingCursor
	}

	if m.Rendered != "" {
		return m.Rendered
	}
	return source
}

func (m *Message) viewToolCall() string {
	if m.cacheWidth == m.Width && m.cacheExpanded == m.Expanded && m.cachedView != "" {
		return m.cachedView
	}
	var b strings.Builder

	icon := "▸"
	if m.Expanded {
		icon = "▾"
	}

	var statusIcon, verb string
	done := m.ToolResult != nil
	if !done {
		statusIcon = m.Styles.ToolCallPendingIcon.Render("◉")
		verb = "Using"
	} else if m.ToolResult.Error != "" {
		statusIcon = m.Styles.ToolCallErrorIcon.Render("✗")
		verb = "Error"
	} else {
		statusIcon = m.Styles.ToolCallDoneIcon.Render("✓")
		verb = "Used"
	}

	header := fmt.Sprintf("%s %s %s %s", icon, statusIcon, verb, m.ToolCall.Name)
	if done && !m.ToolEnd.IsZero() && !m.ToolStart.IsZero() {
		d := m.ToolEnd.Sub(m.ToolStart)
		header += fmt.Sprintf(" (%s)", d.Round(time.Millisecond))
	}

	b.WriteString(m.Styles.ToolCall.Render(header))

	if m.Expanded {
		b.WriteString("\n")
		args := prettyJSONArgs(m.ToolCall.Arguments)
		argsWrapped := wordWrap(args, max(m.Width-messageWidthPadding, minMessageWidth))
		b.WriteString(m.Styles.ToolCallExpanded.Render("Arguments: " + argsWrapped))

		if m.ToolResult != nil {
			b.WriteString("\n")
			if m.ToolResult.Error != "" {
				errWrapped := wordWrap(m.ToolResult.Error, max(m.Width-messageWidthPadding, minMessageWidth))
				b.WriteString(m.Styles.ErrorMessage.Render("Error: " + errWrapped))
			} else if isLineCountTool(m.ToolCall.Name) {
				lines := countLines(m.ToolResult.Output)
				b.WriteString(m.Styles.ToolCallExpanded.Render(fmt.Sprintf("%d lines", lines)))
			} else {
				outWrapped := wordWrap(m.ToolResult.Output, max(m.Width-messageWidthPadding, minMessageWidth))
				b.WriteString(m.Styles.ToolCallExpanded.Render("Output: " + outWrapped))
			}

			if len(m.ToolResult.ContentParts) > 0 {
				b.WriteString("\n")
				parts := contentWithParts("", m.ToolResult.ContentParts)
				partsWrapped := wordWrap(parts, max(m.Width-messageWidthPadding, minMessageWidth))
				b.WriteString(m.Styles.ToolCallExpanded.Render(partsWrapped))
			}
		}
	}

	m.cachedView = b.String()
	m.cacheWidth = m.Width
	m.cacheExpanded = m.Expanded
	return m.cachedView
}

func prettyJSONArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

// isLineCountTool reports whether the tool result should be summarized as a line count.
func isLineCountTool(name string) bool {
	return name == "write_file" || name == "str_replace_file"
}

// countLines returns the number of lines in s, treating an empty string as 0 lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// contentWithParts appends multi-modal content parts (text, images) to a base
// content string. Image parts are rendered as placeholders or links.
func contentWithParts(content string, parts []api.ContentPart) string {
	if len(parts) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(content)
	for _, p := range parts {
		switch p.Type {
		case api.ContentPartText:
			if p.Text != "" {
				b.WriteString("\n\n")
				b.WriteString(p.Text)
			}
		case api.ContentPartImageURL:
			b.WriteString("\n\n🖼️ image")
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				b.WriteString(": ")
				b.WriteString(p.ImageURL.URL)
			}
		case api.ContentPartImageData:
			b.WriteString("\n\n🖼️ image")
		}
	}
	if content == "" {
		return strings.TrimLeft(b.String(), "\n")
	}
	return b.String()
}

func (m *Message) viewError() string {
	if m.cacheWidth == m.Width && m.cachedView != "" {
		return m.cachedView
	}
	prefix := m.Styles.ErrorMessage.Render("Error")
	content := wordWrap(m.Content, max(m.Width-messageWidthPadding, minMessageWidth))
	body := m.Styles.ErrorMessage.Render(content)
	m.cachedView = lipgloss.JoinVertical(lipgloss.Left, prefix, body)
	m.cacheWidth = m.Width
	return m.cachedView
}

func safeGlamourRender(content, theme string, width int) (rendered string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("glamour.Render panicked", "recover", r)
			rendered = content
		}
	}()

	key := fmt.Sprintf("%s:%d", theme, width)
	cr, _ := rendererCache.LoadOrStore(key, &cachedRenderer{})
	c, ok := cr.(*cachedRenderer)
	if !ok {
		return content
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.r == nil {
		var err error
		c.r, err = glamour.NewTermRenderer(
			glamour.WithStandardStyle(theme),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return content
		}
	}

	rendered, err := c.r.Render(content)
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
		// Soft-wrap at word boundaries (spaces and hyphens). ANSI sequences are
		// preserved and wide characters are accounted for.
		soft := ansi.Wordwrap(line, width, " ")
		for _, wrapped := range strings.Split(soft, "\n") {
			// Hard-wrap any line that is still too long (indivisible runs such as
			// very long identifiers, URLs, or CJK text without breakpoints).
			for ansi.StringWidth(wrapped) > width {
				out = append(out, ansi.Cut(wrapped, 0, width))
				wrapped = ansi.Cut(wrapped, width, ansi.StringWidth(wrapped))
			}
			out = append(out, wrapped)
		}
	}
	return strings.Join(out, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
