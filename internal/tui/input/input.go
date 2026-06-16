// Package input provides a multi-line input component for the kimi-lite TUI.
package input

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	Send           key.Binding
	Newline        key.Binding
	ExternalEditor key.Binding
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
		ExternalEditor: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "external editor"),
		),
	}
}

// ConfigurableKeyMap returns a KeyMap from api.KeybindingConfig.
func ConfigurableKeyMap(cfg api.KeybindingConfig) KeyMap {
	km := DefaultKeyMap()
	if cfg.Send != "" {
		km.Send = key.NewBinding(key.WithKeys(cfg.Send), key.WithHelp(cfg.Send, "send"))
	}
	if cfg.Newline != "" {
		km.Newline = key.NewBinding(key.WithKeys(cfg.Newline), key.WithHelp(cfg.Newline, "newline"))
	}
	if cfg.ExternalEditor != "" {
		km.ExternalEditor = key.NewBinding(key.WithKeys(cfg.ExternalEditor), key.WithHelp(cfg.ExternalEditor, "external editor"))
	}
	return km
}

// SlashCommand describes a single slash command available for autocompletion.
type SlashCommand struct {
	Name        string
	Description string
}

// DefaultSlashCommands is the built-in list of slash commands.
var DefaultSlashCommands = []SlashCommand{
	{Name: "/compact", Description: "Summarize older messages to free context"},
	{Name: "/clear", Description: "Clear the current transcript"},
	{Name: "/sessions", Description: "Switch to another session"},
	{Name: "/checkpoint", Description: "Create a git checkpoint commit"},
	{Name: "/diff", Description: "Show git diff for a path"},
	{Name: "/mcp", Description: "List connected MCP tools"},
	{Name: "/title", Description: "Rename the current session"},
	{Name: "/fork", Description: "Fork the current session"},
	{Name: "/help", Description: "Show keyboard shortcuts and commands"},
}

// mentionState tracks an active @-mention completion session.
type mentionState struct {
	start      int      // absolute byte position of '@' in the value
	end        int      // absolute byte position after the current word
	query      string   // lower-cased text after '@'
	candidates []string // matching candidate paths
	selected   int      // index of the selected candidate
}

// slashState tracks an active /-command completion session.
type slashState struct {
	start      int            // absolute byte position of '/' in the value
	end        int            // absolute byte position after the current word
	query      string         // lower-cased text after '/'
	candidates []SlashCommand // matching slash commands
	selected   int            // index of the selected candidate
}

// Model is the input component model.
type Model struct {
	textarea       textarea.Model
	styles         *styles.Styles
	keyMap         KeyMap
	history        []string
	histIdx        int // -1 means current draft, >=0 means history index
	draft          string
	width          int
	maxHistory     int
	editor         string // configured editor; env vars used as fallback
	fileCandidates []string
	candidateFn    func() []string
	mention        *mentionState
	slashCmds      []SlashCommand
	slash          *slashState
	ctx            context.Context
	mu             sync.RWMutex
}

// New creates a new input model.
func New(st *styles.Styles, keyMap KeyMap, maxHistory int) *Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send, Alt+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.Focus()

	m := &Model{
		textarea:   ta,
		styles:     st,
		keyMap:     keyMap,
		history:    make([]string, 0),
		histIdx:    -1,
		maxHistory: maxHistory,
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
	cmd := m.UpdateMsg(msg)
	return m, cmd
}

// UpdateMsg processes a message and returns the resulting command.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	// External editor performs file I/O and subprocess lookup; handle it outside
	// the critical section so the lock is not held during I/O.
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if key.Matches(keyMsg, m.keyMap.ExternalEditor) {
			return m.openExternalEditorCmd()
		}
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Lock for the entire key handling path so mutable state (history,
		// mention selection, editor configuration) is not modified concurrently
		// with SetFileCandidates / SetEditor / SetContext calls.
		m.mu.Lock()
		defer m.mu.Unlock()

		km := m.keyMap

		// @-mention completion navigation takes precedence.
		if m.mention != nil {
			switch msg.String() {
			case "tab", "down":
				m.mention.selected++
				if m.mention.selected >= len(m.mention.candidates) {
					m.mention.selected = 0
				}
				return nil
			case "shift+tab", "up":
				m.mention.selected--
				if m.mention.selected < 0 {
					m.mention.selected = len(m.mention.candidates) - 1
				}
				return nil
			case "enter":
				m.insertCandidate()
				return nil
			case "esc", "ctrl+c":
				m.mention = nil
				return nil
			}
		}

		// /-command completion navigation.
		if m.slash != nil {
			switch msg.String() {
			case "tab", "down":
				m.slash.selected++
				if m.slash.selected >= len(m.slash.candidates) {
					m.slash.selected = 0
				}
				return nil
			case "shift+tab", "up":
				m.slash.selected--
				if m.slash.selected < 0 {
					m.slash.selected = len(m.slash.candidates) - 1
				}
				return nil
			case "enter":
				m.insertSlashCandidate()
				return nil
			case "esc", "ctrl+c":
				m.slash = nil
				return nil
			}
		}

		if key.Matches(msg, km.Send) {
			content := strings.TrimSpace(m.textarea.Value())
			if content != "" {
				// De-duplicate consecutive entries.
				if len(m.history) == 0 || m.history[len(m.history)-1] != content {
					m.history = append(m.history, content)
					// Trim oldest entries beyond the cap.
					if m.maxHistory > 0 && len(m.history) > m.maxHistory {
						m.history = m.history[len(m.history)-m.maxHistory:]
					}
				}
				m.histIdx = -1
				m.draft = ""
				m.textarea.Reset()
				m.mention = nil
				m.slash = nil
				return func() tea.Msg {
					return SendMsg{Content: content}
				}
			}
			return nil
		}

		if key.Matches(msg, km.Newline) {
			m.textarea.InsertString("\n")
			m.detectMention()
			m.detectSlash()
			return nil
		}

		// History navigation
		if msg.String() == "up" || msg.String() == "ctrl+p" {
			if len(m.history) == 0 {
				return nil
			}
			if m.histIdx == -1 {
				m.draft = m.textarea.Value()
				m.histIdx = len(m.history) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.textarea.SetValue(m.history[m.histIdx])
			m.textarea.CursorEnd()
			m.mention = nil
			m.slash = nil
			return tea.Batch(cmds...)
		}

		if msg.String() == "down" || msg.String() == "ctrl+n" {
			if m.histIdx == -1 {
				return nil
			}
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.textarea.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = -1
				m.textarea.SetValue(m.draft)
			}
			m.textarea.CursorEnd()
			m.mention = nil
			m.slash = nil
			return tea.Batch(cmds...)
		}

		// Key was not handled above; pass it to the textarea below while still
		// holding the lock so concurrent SetFileCandidates/SetEditor calls do
		// not interleave with the update.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		m.detectMention()
		m.detectSlash()
		return tea.Batch(cmds...)

	case editorFinishedMsg:
		m.mu.Lock()
		defer m.mu.Unlock()
		m.handleEditorFinished(msg)
		m.detectMention()
		m.detectSlash()
		return tea.Batch(cmds...)
	}

	// Pass other messages to textarea
	m.mu.Lock()
	defer m.mu.Unlock()
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)
	m.detectMention()
	m.detectSlash()
	return tea.Batch(cmds...)
}

// openExternalEditorCmd returns a command that launches the external editor
// without holding m.mu during file I/O or exec.LookPath.
func (m *Model) openExternalEditorCmd() tea.Cmd {
	m.mu.Lock()
	content := m.textarea.Value()
	editor := m.editor
	ctx := m.ctx
	m.mu.Unlock()
	return m.openExternalEditor(ctx, editor, content)
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view := m.styles.InputBox.Render(m.textarea.View())
	if comp := m.completionView(); comp != "" {
		view += "\n" + comp
	}
	return tea.NewView(view)
}

// Height returns the rendered height of the input component.
func (m *Model) Height() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view := m.styles.InputBox.Render(m.textarea.View())
	if comp := m.completionView(); comp != "" {
		view += "\n" + comp
	}
	return lipgloss.Height(view)
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

// Reset clears the input and any transient state.
func (m *Model) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.textarea.Reset()
	m.mention = nil
	m.slash = nil
	m.draft = ""
	m.histIdx = -1
}

// SetValue sets the input value.
func (m *Model) SetValue(s string) {
	m.textarea.SetValue(s)
}

// SetEditor sets the external editor command. An empty value falls back to
// $VISUAL, $EDITOR, or vi at trigger time.
func (m *Model) SetEditor(editor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.editor = editor
}

// SetCandidateFunc sets a function that returns fresh file candidates on demand.
func (m *Model) SetCandidateFunc(fn func() []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candidateFn = fn
}

// SetContext sets the context used to control the external editor subprocess.
// When the context is cancelled, the running editor process is terminated.
func (m *Model) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
}

// SetFileCandidates sets the list of file paths available for @-mention
// completion.
func (m *Model) SetFileCandidates(paths []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileCandidates = make([]string, len(paths))
	copy(m.fileCandidates, paths)
	if m.mention != nil {
		m.detectMention()
	}
}

// SetSlashCommands sets the list of commands shown for /-completion.
func (m *Model) SetSlashCommands(cmds []SlashCommand) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slashCmds = make([]SlashCommand, len(cmds))
	copy(m.slashCmds, cmds)
}

// Completing reports whether a completion popup is currently open.
func (m *Model) Completing() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mention != nil || m.slash != nil
}

// CloseCompletion dismisses any open completion popup.
func (m *Model) CloseCompletion() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mention = nil
	m.slash = nil
}

// cursorPosition returns the absolute byte position of the cursor in the
// current value.
func (m *Model) cursorPosition() int {
	value := m.textarea.Value()
	line := m.textarea.Line()
	if line < 0 {
		line = 0
	}
	lines := strings.Split(value, "\n")
	if line >= len(lines) {
		return len(value)
	}
	pos := 0
	for i := 0; i < line; i++ {
		pos += len(lines[i]) + 1 // '\n'
	}
	li := m.textarea.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	pos += len(string(runes[:col]))
	return pos
}

// wordAtCursor returns the word surrounding the cursor and its byte range in
// the current value. Word boundaries are whitespace characters. All cursor and
// boundary logic is rune-based so multi-byte characters are handled correctly.
func (m *Model) wordAtCursor() (word string, start, end int) {
	value := m.textarea.Value()
	runes := []rune(value)
	pos := m.cursorPosition()
	if pos < 0 {
		pos = 0
	}
	if pos > len(value) {
		pos = len(value)
	}

	// Convert byte cursor position to a rune index.
	rpos := 0
	for i := range value {
		if i >= pos {
			break
		}
		rpos++
	}

	startRune := rpos
	for startRune > 0 && !isWordBoundary(runes[startRune-1]) {
		startRune--
	}
	endRune := rpos
	for endRune < len(runes) && !isWordBoundary(runes[endRune]) {
		endRune++
	}

	start = runeOffsetToByte(value, startRune)
	end = runeOffsetToByte(value, endRune)
	return value[start:end], start, end
}

// runeOffsetToByte returns the byte offset in s that corresponds to the given
// rune offset.
func runeOffsetToByte(s string, runeOffset int) int {
	count := 0
	for i := range s {
		if count == runeOffset {
			return i
		}
		count++
	}
	return len(s)
}

func isWordBoundary(r rune) bool {
	return unicode.IsSpace(r)
}

// detectMention updates the active mention state based on the current word at
// the cursor.
func (m *Model) detectMention() {
	word, start, end := m.wordAtCursor()
	if !strings.HasPrefix(word, "@") {
		m.mention = nil
		return
	}
	query := strings.ToLower(word[1:])
	if len(m.fileCandidates) == 0 && m.candidateFn != nil {
		m.fileCandidates = m.candidateFn()
	}
	candidates := m.filterCandidates(query)
	if len(candidates) == 0 {
		m.mention = nil
		return
	}
	m.mention = &mentionState{
		start:      start,
		end:        end,
		query:      query,
		candidates: candidates,
		selected:   0,
	}
}

// filterCandidates returns file candidates matching the lower-cased query.
// A candidate matches when its full path or base name has the query as a
// case-insensitive prefix.
func (m *Model) filterCandidates(query string) []string {
	query = strings.ToLower(query)
	seen := make(map[string]struct{})
	var matches []string
	for _, c := range m.fileCandidates {
		lower := strings.ToLower(c)
		if _, ok := seen[c]; ok {
			continue
		}
		if strings.HasPrefix(lower, query) {
			seen[c] = struct{}{}
			matches = append(matches, c)
			continue
		}
		if strings.HasPrefix(filepath.Base(lower), query) {
			seen[c] = struct{}{}
			matches = append(matches, c)
		}
	}
	sort.Strings(matches)
	return matches
}

// insertCandidate replaces the active @-mention word with the selected path.
func (m *Model) insertCandidate() {
	if m.mention == nil {
		return
	}
	value := m.textarea.Value()
	replacement := "@" + m.mention.candidates[m.mention.selected]
	newValue := value[:m.mention.start] + replacement
	if m.mention.end <= len(value) {
		newValue += value[m.mention.end:]
	}
	m.textarea.SetValue(newValue)
	m.mention = nil
}

// detectSlash updates the active slash state based on the current word at the
// cursor.
func (m *Model) detectSlash() {
	word, start, end := m.wordAtCursor()
	if !strings.HasPrefix(word, "/") {
		m.slash = nil
		return
	}
	query := strings.ToLower(word[1:])
	candidates := m.filterSlashCandidates(query)
	if len(candidates) == 0 {
		m.slash = nil
		return
	}
	m.slash = &slashState{
		start:      start,
		end:        end,
		query:      query,
		candidates: candidates,
		selected:   0,
	}
}

// filterSlashCandidates returns slash commands matching the lower-cased query.
func (m *Model) filterSlashCandidates(query string) []SlashCommand {
	query = strings.ToLower(query)
	var matches []SlashCommand
	for _, c := range m.slashCmds {
		name := strings.ToLower(c.Name)
		if strings.HasPrefix(name, query) || strings.Contains(name, query) {
			matches = append(matches, c)
		}
	}
	return matches
}

// insertSlashCandidate replaces the active slash word with the selected command
// followed by a trailing space.
func (m *Model) insertSlashCandidate() {
	if m.slash == nil {
		return
	}
	value := m.textarea.Value()
	replacement := m.slash.candidates[m.slash.selected].Name + " "
	newValue := value[:m.slash.start] + replacement
	if m.slash.end <= len(value) {
		newValue += value[m.slash.end:]
	}
	m.textarea.SetValue(newValue)
	m.slash = nil
}

// completionView renders the completion popup or an empty string if none is
// active.
func (m *Model) completionView() string {
	if comp := m.mentionCompletionView(); comp != "" {
		return comp
	}
	return m.slashCompletionView()
}

// mentionCompletionView renders the @-mention completion popup.
func (m *Model) mentionCompletionView() string {
	if m.mention == nil || len(m.mention.candidates) == 0 || m.styles == nil {
		return ""
	}
	var b strings.Builder
	const maxItems = 8
	for i, c := range m.mention.candidates {
		if i >= maxItems {
			break
		}
		prefix := "  "
		if i == m.mention.selected {
			prefix = "> "
		}
		b.WriteString(prefix + c + "\n")
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if content == "" {
		return ""
	}
	return m.styles.MentionPopup.Render(content)
}

// slashCompletionView renders the /-command completion popup.
func (m *Model) slashCompletionView() string {
	if m.slash == nil || len(m.slash.candidates) == 0 || m.styles == nil {
		return ""
	}
	var b strings.Builder
	const maxItems = 8
	for i, c := range m.slash.candidates {
		if i >= maxItems {
			break
		}
		prefix := "  "
		if i == m.slash.selected {
			prefix = "> "
		}
		line1 := prefix + c.Name
		line2 := "    " + c.Description
		b.WriteString(m.styles.SlashCommandName.Render(line1) + "\n")
		b.WriteString(m.styles.SlashCommandDesc.Render(line2) + "\n")
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if content == "" {
		return ""
	}
	return m.styles.SlashPopup.Render(content)
}

func (m *Model) updateStyles() {
	if m.styles == nil {
		return
	}
	s := m.textarea.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Base = lipgloss.NewStyle().
		Background(m.styles.Theme.InputBg).
		Foreground(m.styles.Theme.Foreground)
	s.Blurred.Base = lipgloss.NewStyle().
		Background(m.styles.Theme.InputBg).
		Foreground(m.styles.Theme.Muted)
	m.textarea.SetStyles(s)
}
