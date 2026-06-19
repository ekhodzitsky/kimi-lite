// Package input provides a multi-line input component for the kimi-lite TUI.
package input

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/clipboard"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	inputWidthPadding = 4
	popupMaxItems     = 8

	defaultPlaceholder = "Type a message... (Enter to send, Alt+Enter/Shift+Enter/Ctrl+J for newline)"
)

// SendMsg is emitted when the user wants to send the current input.
type SendMsg struct {
	Content      string
	ContentParts []api.ContentPart
}

// PasteMsg is emitted when a paste operation completed and produced attachments.
type PasteMsg struct {
	Parts []api.ContentPart
}

// PasteErrorMsg is emitted when a paste operation failed.
type PasteErrorMsg struct {
	Err error
}

// KeyMap defines keybindings for the input component.
type KeyMap struct {
	Send           key.Binding
	Newline        key.Binding
	ExternalEditor key.Binding
	Paste          key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Newline: key.NewBinding(
			key.WithKeys("alt+enter", "shift+enter", "ctrl+j"),
			key.WithHelp("alt+enter/shift+enter/ctrl+j", "newline"),
		),
		ExternalEditor: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "external editor"),
		),
		Paste: key.NewBinding(
			key.WithKeys("ctrl+v", "alt+v"),
			key.WithHelp("ctrl+v/alt+v", "paste image or file"),
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
	if cfg.Paste != "" {
		km.Paste = key.NewBinding(key.WithKeys(cfg.Paste), key.WithHelp(cfg.Paste, "paste image or file"))
	}
	return km
}

// SlashCommand describes a single slash command available for autocompletion.
type SlashCommand struct {
	Name         string
	Description  string
	NoAutoSubmit bool // when true, selecting the command leaves it in the input for argument entry
}

// DefaultSlashCommands is the built-in list of slash commands.
var DefaultSlashCommands = []SlashCommand{
	{Name: "/compact", Description: "Summarize older messages to free context"},
	{Name: "/clear", Description: "Clear the current transcript"},
	{Name: "/sessions", Description: "Switch to another session"},
	{Name: "/resume", Description: "Switch to another session (alias for /sessions)"},
	{Name: "/checkpoint", Description: "Create a git checkpoint commit"},
	{Name: "/diff", Description: "Show git diff for a path", NoAutoSubmit: true},
	{Name: "/mcp", Description: "List connected MCP tools"},
	{Name: "/title", Description: "Rename the current session", NoAutoSubmit: true},
	{Name: "/fork", Description: "Fork the current session"},
	{Name: "/export", Description: "Export the current session to JSON", NoAutoSubmit: true},
	{Name: "/import", Description: "Import a session JSON snapshot", NoAutoSubmit: true},
	{Name: "/model", Description: "Switch the active LLM model", NoAutoSubmit: true},
	{Name: "/goal", Description: "Set a short-term goal for this session", NoAutoSubmit: true},
	{Name: "/btw", Description: "Queue a note for the next message", NoAutoSubmit: true},
	{Name: "/version", Description: "Show the build version"},
	{Name: "/help", Description: "Show keyboard shortcuts and commands"},
}

// mentionState tracks an active @-mention completion session.
type mentionState struct {
	start      int      // absolute byte position of '@' in the value
	end        int      // absolute byte position after the current word
	query      string   // lower-cased text after '@'
	candidates []string // matching candidate paths
	selected   int      // index of the selected candidate
	scroll     int      // index of the first visible candidate
}

// slashState tracks an active /-command completion session.
type slashState struct {
	start      int            // absolute byte position of '/' in the value
	end        int            // absolute byte position after the current word
	query      string         // lower-cased text after '/'
	candidates []SlashCommand // matching slash commands
	selected   int            // index of the selected candidate
	scroll     int            // index of the first visible candidate
}

// clampScroll ensures the selected item is visible and the view stays within
// the candidate list.
func (s *mentionState) clampScroll() {
	if s.selected < s.scroll {
		s.scroll = s.selected
	}
	if s.selected >= s.scroll+popupMaxItems {
		s.scroll = s.selected - popupMaxItems + 1
	}
	maxScroll := len(s.candidates) - popupMaxItems
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.scroll > maxScroll {
		s.scroll = maxScroll
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
}

// clampScroll ensures the selected item is visible and the view stays within
// the candidate list.
func (s *slashState) clampScroll() {
	if s.selected < s.scroll {
		s.scroll = s.selected
	}
	if s.selected >= s.scroll+popupMaxItems {
		s.scroll = s.selected - popupMaxItems + 1
	}
	maxScroll := len(s.candidates) - popupMaxItems
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.scroll > maxScroll {
		s.scroll = maxScroll
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
}

// Attachment holds a pasted file attachment.
type Attachment struct {
	Path     string
	MIMEType string
}

// CandidatesRefreshedMsg is emitted when the asynchronous candidate refresh
// completes.
type CandidatesRefreshedMsg struct {
	Candidates []string
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
	configDir      string // directory for temporary pasted files
	fileCandidates []string
	candidateFn    func() []string
	mention        *mentionState
	slashCmds      []SlashCommand
	slash          *slashState
	ctx            context.Context
	planMode       bool
	attachments    []Attachment
	mu             sync.RWMutex
}

// New creates a new input model.
func New(st *styles.Styles, keyMap KeyMap, maxHistory int) *Model {
	ta := textarea.New()
	ta.Placeholder = defaultPlaceholder
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

	// External editor and paste perform file I/O and subprocess lookup; handle
	// them outside the critical section so the lock is not held during I/O.
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if key.Matches(keyMsg, m.keyMap.ExternalEditor) {
			return m.openExternalEditorCmd()
		}
		if key.Matches(keyMsg, m.keyMap.Paste) {
			return m.pasteCmd()
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
					m.mention.scroll = 0
				}
				m.mention.clampScroll()
				return nil
			case "shift+tab", "up":
				m.mention.selected--
				if m.mention.selected < 0 {
					m.mention.selected = len(m.mention.candidates) - 1
				}
				m.mention.clampScroll()
				return nil
			case "pgdown":
				m.mention.selected += popupMaxItems
				if m.mention.selected >= len(m.mention.candidates) {
					m.mention.selected = len(m.mention.candidates) - 1
				}
				m.mention.clampScroll()
				return nil
			case "pgup":
				m.mention.selected -= popupMaxItems
				if m.mention.selected < 0 {
					m.mention.selected = 0
				}
				m.mention.clampScroll()
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
					m.slash.scroll = 0
				}
				m.slash.clampScroll()
				return nil
			case "shift+tab", "up":
				m.slash.selected--
				if m.slash.selected < 0 {
					m.slash.selected = len(m.slash.candidates) - 1
				}
				m.slash.clampScroll()
				return nil
			case "pgdown":
				m.slash.selected += popupMaxItems
				if m.slash.selected >= len(m.slash.candidates) {
					m.slash.selected = len(m.slash.candidates) - 1
				}
				m.slash.clampScroll()
				return nil
			case "pgup":
				m.slash.selected -= popupMaxItems
				if m.slash.selected < 0 {
					m.slash.selected = 0
				}
				m.slash.clampScroll()
				return nil
			case "enter":
				selected := m.slash.candidates[m.slash.selected]
				m.insertSlashCandidate()
				if selected.NoAutoSubmit {
					return nil
				}
				return m.submitCurrentLocked()
			case "esc", "ctrl+c":
				m.slash = nil
				return nil
			}
		}

		if key.Matches(msg, km.Send) {
			return m.submitCurrentLocked()
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

	case PasteMsg:
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, part := range msg.Parts {
			m.addContentPart(part)
		}
		return tea.Batch(cmds...)

	case PasteErrorMsg:
		return func() tea.Msg {
			return PasteErrorMsg{Err: fmt.Errorf("paste failed: %w", msg.Err)}
		}

	case editorFinishedMsg:
		m.mu.Lock()
		defer m.mu.Unlock()
		m.handleEditorFinished(msg)
		m.detectMention()
		m.detectSlash()
		return tea.Batch(cmds...)

	case CandidatesRefreshedMsg:
		m.SetFileCandidates(msg.Candidates)
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
	var b strings.Builder
	if m.planMode {
		b.WriteString(m.styles.PlanModeIndicator.Render("[PLAN] Press Shift+Tab to disable plan mode") + "\n")
	}
	b.WriteString(m.styles.InputBox.Render(m.textarea.View()))
	if att := m.attachmentView(); att != "" {
		b.WriteString("\n" + att)
	}
	if comp := m.completionView(); comp != "" {
		b.WriteString("\n" + comp)
	}
	return tea.NewView(b.String())
}

// Height returns the rendered height of the input component.
func (m *Model) Height() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var b strings.Builder
	if m.planMode {
		b.WriteString(m.styles.PlanModeIndicator.Render("[PLAN] Press Shift+Tab to disable plan mode") + "\n")
	}
	b.WriteString(m.styles.InputBox.Render(m.textarea.View()))
	if att := m.attachmentView(); att != "" {
		b.WriteString("\n" + att)
	}
	if comp := m.completionView(); comp != "" {
		b.WriteString("\n" + comp)
	}
	return lipgloss.Height(b.String())
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
	m.attachments = nil
}

// SetValue sets the input value.
func (m *Model) SetValue(s string) {
	m.textarea.SetValue(s)
}

// SetQueueCount updates the placeholder to include a queued-message indicator.
// A count of zero restores the default placeholder.
func (m *Model) SetQueueCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if count > 0 {
		m.textarea.Placeholder = fmt.Sprintf("%s [Queued: %d]", defaultPlaceholder, count)
	} else {
		m.textarea.Placeholder = defaultPlaceholder
	}
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

// RefreshCandidatesCmd returns a command that loads file candidates from the
// configured candidate function and emits a CandidatesRefreshedMsg.
func (m *Model) RefreshCandidatesCmd() tea.Cmd {
	m.mu.RLock()
	fn := m.candidateFn
	m.mu.RUnlock()
	return func() tea.Msg {
		if fn == nil {
			return CandidatesRefreshedMsg{}
		}
		return CandidatesRefreshedMsg{Candidates: fn()}
	}
}

// SetContext sets the context used to control the external editor subprocess.
// When the context is cancelled, the running editor process is terminated.
func (m *Model) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
}

// SetConfigDir sets the directory used to store temporary pasted files.
func (m *Model) SetConfigDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configDir = dir
}

// Attachments returns the current pasted file attachments.
func (m *Model) Attachments() []Attachment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Attachment(nil), m.attachments...)
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

// submitCurrentLocked sends the current input value and clears transient state.
// The caller must hold m.mu.
func (m *Model) submitCurrentLocked() tea.Cmd {
	content := strings.TrimSpace(m.textarea.Value())
	if content == "" {
		return nil
	}
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
	parts := m.attachmentContentParts()
	m.textarea.Reset()
	m.mention = nil
	m.slash = nil
	m.attachments = nil
	return func() tea.Msg {
		return SendMsg{Content: content, ContentParts: parts}
	}
}

// TogglePlanMode toggles plan mode on/off.
func (m *Model) TogglePlanMode() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planMode = !m.planMode
}

// PlanMode reports whether plan mode is active.
func (m *Model) PlanMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.planMode
}

// SetPlanMode sets plan mode explicitly.
func (m *Model) SetPlanMode(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planMode = v
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
		scroll:     0,
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
	replacement := "@" + m.mention.candidates[m.mention.selected] + " "
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
		scroll:     0,
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
	offset := m.mention.scroll
	if offset < 0 {
		offset = 0
	}
	end := offset + popupMaxItems
	if end > len(m.mention.candidates) {
		end = len(m.mention.candidates)
	}
	for i := offset; i < end; i++ {
		line := "  " + m.mention.candidates[i]
		if i == m.mention.selected {
			line = m.styles.CompletionSelected.Render("> " + m.mention.candidates[i])
		}
		b.WriteString(line + "\n")
	}
	if remaining := len(m.mention.candidates) - end; remaining > 0 {
		fmt.Fprintf(&b, "  … %d more\n", remaining)
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
	offset := m.slash.scroll
	if offset < 0 {
		offset = 0
	}
	end := offset + popupMaxItems
	if end > len(m.slash.candidates) {
		end = len(m.slash.candidates)
	}
	for i := offset; i < end; i++ {
		c := m.slash.candidates[i]
		selected := i == m.slash.selected
		line1 := "> " + c.Name
		line2 := "    " + c.Description
		if selected {
			b.WriteString(m.styles.CompletionSelected.Render(line1) + "\n")
			b.WriteString(m.styles.CompletionSelected.Render(line2) + "\n")
		} else {
			line1 = "  " + c.Name
			b.WriteString(m.styles.SlashCommandName.Render(line1) + "\n")
			b.WriteString(m.styles.SlashCommandDesc.Render(line2) + "\n")
		}
	}
	if remaining := len(m.slash.candidates) - end; remaining > 0 {
		fmt.Fprintf(&b, "  … %d more\n", remaining)
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
	s.Focused.Base = m.styles.InputBoxFocused
	s.Blurred.Base = lipgloss.NewStyle().
		Background(m.styles.Theme.InputBg).
		Foreground(m.styles.Theme.Muted)
	m.textarea.SetStyles(s)
}

// clipboardReaderFn is a test seam for clipboard access.
var (
	readImageFn      = clipboard.ReadImage
	readFilePathsFn  = clipboard.ReadFilePaths
	copyFileToTempFn = clipboard.CopyFileToTemp
	saveDataFn       = clipboard.SaveData
)

// pasteCmd reads the clipboard asynchronously and returns a PasteMsg with any
// image or file attachments. It runs outside the model lock because clipboard
// I/O may block.
func (m *Model) pasteCmd() tea.Cmd {
	m.mu.RLock()
	ctx := m.ctx
	configDir := m.configDir
	m.mu.RUnlock()
	if configDir == "" {
		return nil
	}
	return func() tea.Msg {
		parts, err := readClipboardAttachments(ctx, configDir)
		if err != nil {
			return PasteErrorMsg{Err: err}
		}
		return PasteMsg{Parts: parts}
	}
}

// readClipboardAttachments attempts to read an image or file list from the
// clipboard and returns them as content parts. File-path attachments are copied
// into configDir/tmp so they stay within the LLM attachment sandbox.
func readClipboardAttachments(ctx context.Context, configDir string) ([]api.ContentPart, error) {
	data, mime, err := readImageFn(ctx)
	if err == nil && len(data) > 0 {
		path, saveErr := saveDataFn(data, clipboard.ExtensionForMIME(mime), configDir)
		if saveErr == nil {
			return []api.ContentPart{{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: path}}}, nil
		}
		return nil, saveErr
	}

	paths, err := readFilePathsFn(ctx)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	parts := make([]api.ContentPart, 0, len(paths))
	for _, path := range paths {
		mime := clipboard.MIMEForPath(path)
		tmpPath, copyErr := copyFileToTempFn(path, configDir)
		if copyErr != nil {
			return nil, copyErr
		}
		if strings.HasPrefix(mime, "image/") {
			parts = append(parts, api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: tmpPath}})
		} else {
			parts = append(parts, api.ContentPart{Type: api.ContentPartText, Text: "[Attached file: " + tmpPath + "]"})
		}
	}
	return parts, nil
}

// addContentPart stores a pasted content part as a file attachment if it
// references a local path. For text markers that encode an attached file path,
// the path is extracted and stored so the marker is re-emitted on send.
func (m *Model) addContentPart(part api.ContentPart) {
	switch part.Type {
	case api.ContentPartImageURL:
		if part.ImageURL != nil && part.ImageURL.URL != "" {
			mime := clipboard.MIMEForPath(part.ImageURL.URL)
			m.attachments = append(m.attachments, Attachment{Path: part.ImageURL.URL, MIMEType: mime})
		}
	case api.ContentPartText:
		if part.Text == "" {
			return
		}
		if path, ok := parseAttachedFileMarker(part.Text); ok {
			mime := clipboard.MIMEForPath(path)
			m.attachments = append(m.attachments, Attachment{Path: path, MIMEType: mime})
		} else {
			// Plain text without a file path is pasted directly into the input
			// so it is not silently dropped.
			m.textarea.InsertString(part.Text)
		}
	}
}

// parseAttachedFileMarker extracts the file path from a marker like
// "[Attached file: /path/to/file]".
func parseAttachedFileMarker(text string) (string, bool) {
	const prefix = "[Attached file: "
	const suffix = "]"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return "", false
	}
	path := strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix)
	return path, path != ""
}

// attachmentContentParts converts stored attachments to content parts.
func (m *Model) attachmentContentParts() []api.ContentPart {
	if len(m.attachments) == 0 {
		return nil
	}
	parts := make([]api.ContentPart, 0, len(m.attachments))
	for _, att := range m.attachments {
		if strings.HasPrefix(att.MIMEType, "image/") {
			parts = append(parts, api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: att.Path}})
		} else if att.Path != "" {
			parts = append(parts, api.ContentPart{Type: api.ContentPartText, Text: "[Attached file: " + att.Path + "]"})
		}
	}
	return parts
}

// attachmentView renders a one-line indicator for pasted attachments.
func (m *Model) attachmentView() string {
	if len(m.attachments) == 0 || m.styles == nil {
		return ""
	}
	var b strings.Builder
	for i, att := range m.attachments {
		if i > 0 {
			b.WriteString(" ")
		}
		name := att.Path
		if name == "" {
			name = "attachment"
		} else {
			name = filepath.Base(name)
		}
		b.WriteString("📎 " + name)
	}
	return m.styles.Attachment.Render(b.String())
}
