// Package tui provides the Terminal UI layer for kimi-lite using Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	gloss "charm.land/lipgloss/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/sidebar"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/viewport"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// focusedComponent tracks which TUI component has keyboard focus.
type focusedComponent int

const (
	focusInput focusedComponent = iota
	focusSidebar
	focusViewport
)

const (
	statusHeight      = 1
	minContentWidth   = 20
	minViewportHeight = 5
	// viewportWidthPadding accounts for sidebar border + content padding.
	viewportWidthPadding = 4
	minInputHeight       = 3

	resizeDebounce = 80 * time.Millisecond
)

// inputHeight returns the current rendered height of the input component.
func (m *Model) inputHeight() int {
	h := m.input.Height()
	if h < minInputHeight {
		return minInputHeight
	}
	return h
}

// layoutRect holds computed geometry for a single frame.
type layoutRect struct {
	sbWidth      int
	contentWidth int
	vpWidth      int
	vpHeight     int
	inputHeight  int
	statusY      int
}

// eq reports whether two layouts have identical geometry.
func (r layoutRect) eq(o layoutRect) bool {
	return r.sbWidth == o.sbWidth &&
		r.contentWidth == o.contentWidth &&
		r.vpWidth == o.vpWidth &&
		r.vpHeight == o.vpHeight &&
		r.inputHeight == o.inputHeight &&
		r.statusY == o.statusY
}

// layout computes all geometry once, folding in min-content/min-viewport clamps.
func (m *Model) layout() layoutRect {
	sbWidth := 0
	if m.sidebar.Visible() {
		sbWidth = m.sidebar.Width()
	}
	contentWidth := m.width - sbWidth
	if contentWidth < minContentWidth {
		contentWidth = minContentWidth
	}
	inputHeight := m.inputHeight()
	vpHeight := m.height - statusHeight - inputHeight
	if vpHeight < minViewportHeight {
		vpHeight = minViewportHeight
	}
	return layoutRect{
		sbWidth:      sbWidth,
		contentWidth: contentWidth,
		vpWidth:      contentWidth - viewportWidthPadding,
		vpHeight:     vpHeight,
		inputHeight:  inputHeight,
		statusY:      vpHeight + inputHeight,
	}
}

// turnManager is the interface needed from core.TurnManager.
type turnManager interface {
	RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error)
	ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error
}

// sessionManager is the interface needed from core.SessionManager.
type sessionManager interface {
	CurrentSessionID() string
	ClearMessages(ctx context.Context, id string) error
	Rename(ctx context.Context, id string, name string) error
	Fork(ctx context.Context, sourceID string, name string) (*api.Session, error)
}

// compressor is the interface needed from core.ContextCompressor.
type compressor interface {
	Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error)
}

// Model is the root Bubble Tea model composing all child models.
type Model struct {
	styles *styles.Styles
	config *api.Config

	input    *input.Model
	vp       *viewport.Model
	sidebar  *sidebar.Model
	messages []*msgcomp.Message

	state       api.TurnState
	session     *api.Session
	modelName   string
	contextUsed int
	contextMax  int
	toolCount   int

	width  int
	height int

	// approvalController owns the pending tool-call approval state machine.
	approval *approvalController

	// Service references (optional, wired by app layer)
	turnManager    turnManager
	sessionManager sessionManager
	compressor     compressor
	gitProvider    api.GitProvider
	mcpClient      api.MCPClient
	store          api.Store

	// Approval callbacks (wired by app layer)
	autoApproveSetter  func(string)
	approvalModeSetter func(int)
	approvalMode       int // 1=ModeAuto, 2=ModeYolo

	// Streaming state
	streamCh       <-chan api.TurnEvent
	streamCancel   context.CancelFunc
	streamCanceled bool // true after cancel/error until a new stream starts

	// Focus management
	focused focusedComponent

	// appCtx is the program-scoped context that tea.Cmd closures derive from
	// because Bubble Tea's Update signature precludes per-call context passing.
	appCtx context.Context

	// renderBuffer owns renderedContent and incremental-render bookkeeping.
	rb *renderBuffer

	// lastLayout is the most recent layout for which the transcript was rebuilt.
	lastLayout layoutRect
	// pendingResize tracks whether a debounced rebuild is already scheduled.
	pendingResize bool
	// resizeGen increments for each scheduled debounced rebuild.
	resizeGen int
	// rebuildCount is used by tests to verify the number of full transcript rebuilds.
	rebuildCount int

	mu sync.RWMutex
}

// New creates the root TUI model.
func New(cfg *api.Config, session *api.Session, appCtx context.Context) (*Model, error) {
	st := styles.New(cfg.UI.Theme)

	inp := input.New(st, input.ConfigurableKeyMap(cfg.Keybindings), cfg.Session.MaxHistory)
	inp.SetEditor(cfg.UI.Editor)
	vp := viewport.New(st)

	sb, err := sidebar.New(st, session.Path)
	if err != nil {
		return nil, fmt.Errorf("create sidebar: %w", err)
	}

	m := &Model{
		styles:       st,
		config:       cfg,
		input:        inp,
		vp:           vp,
		sidebar:      sb,
		messages:     make([]*msgcomp.Message, 0),
		state:        api.TurnIdle,
		session:      session,
		contextMax:   0,
		modelName:    cfg.LLM.Model,
		focused:      focusInput,
		appCtx:       appCtx,
		rb:           newRenderBuffer(),
		approval:     newApprovalController(),
		approvalMode: 1,
	}
	m.updateLayout()
	m.syncInputCandidates()
	return m, nil
}

// SetTurnManager wires the turn manager for executing LLM turns.
func (m *Model) SetTurnManager(tm turnManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turnManager = tm
}

// SetSessionManager wires the session manager.
func (m *Model) SetSessionManager(sm sessionManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionManager = sm
}

// SetCompressor wires the context compressor.
func (m *Model) SetCompressor(c compressor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compressor = c
}

// SetGitProvider wires the git provider.
func (m *Model) SetGitProvider(gp api.GitProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gitProvider = gp
}

// SetMCPClient wires the MCP client.
func (m *Model) SetMCPClient(mc api.MCPClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mcpClient = mc
}

// SetStore wires the store for direct operations.
func (m *Model) SetStore(st api.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = st
}

// SetAutoApproveSetter wires the callback for adding a tool to auto-approve.
func (m *Model) SetAutoApproveSetter(fn func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoApproveSetter = fn
}

// SetApprovalModeSetter wires the callback for toggling the approval mode.
func (m *Model) SetApprovalModeSetter(fn func(int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalModeSetter = fn
}

// SetApprovalMode sets the current approval mode display value.
func (m *Model) SetApprovalMode(mode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalMode = mode
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Init(),
		m.vp.Init(),
		m.sidebar.Init(),
	)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Let the input component consume completion keys while a popup is open.
		if !m.input.Completing() {
			cmds = append(cmds, m.handleKeyMsg(msg)...)
		}
	case tea.MouseMsg:
		m.handleMouseMsg(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		l := m.layout()
		m.applyLayoutSizes(l)
		if !m.pendingResize && !l.eq(m.lastLayout) {
			m.pendingResize = true
			m.resizeGen++
			g := m.resizeGen
			cmds = append(cmds, tea.Tick(resizeDebounce, func(time.Time) tea.Msg {
				return debouncedResizeMsg{gen: g}
			}))
		}

	case debouncedResizeMsg:
		if msg.gen == m.resizeGen {
			m.pendingResize = false
			m.updateLayout()
		}

	case StreamChunkMsg:
		cmds = append(cmds, m.handleStreamChunk(msg.Chunk)...)

	case ToolCallMsg:
		cmds = append(cmds, m.handleToolCalls(msg.Calls)...)

	case ToolResultMsg:
		cmds = append(cmds, m.handleToolResult(msg.Result)...)

	case ErrorMsg:
		m.addMessage(msgcomp.NewErrorMessage(msg.Err, m.styles))
		m.setState(api.TurnError)

	case StateChangeMsg:
		m.setState(msg.State)

	case CompactResultMsg:
		if msg.Count == 0 {
			m.addMessage(msgcomp.NewUserMessage("Nothing to compact", m.styles))
		} else {
			m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Compacted %d messages into a summary", msg.Count), m.styles))
		}
		m.setState(api.TurnIdle)

	case SetTitleMsg:
		if m.session != nil {
			m.session.Name = msg.Name
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Session renamed to %q.", msg.Name), m.styles))
		m.setState(api.TurnIdle)

	case ForkResultMsg:
		if msg.Session != nil {
			m.session = msg.Session
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Forked to session %q (%s).", m.session.Name, m.session.ID), m.styles))
		m.setState(api.TurnIdle)

	case ApprovalRequestMsg:
		m.approval.startRequest(msg.Calls, msg.RequestID)
		m.setState(api.TurnWaitingApproval)
		cmds = append(cmds, m.readStreamChunk())

	case ApprovalResponseMsg:
		cmds = append(cmds, m.handleApprovalResponse(msg)...)

	case SessionsMsg:
		cmds = append(cmds, m.handleSessions()...)

	case CheckpointMsg:
		if m.gitProvider != nil {
			if err := m.gitProvider.Commit(m.appCtx, ""); err != nil {
				m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("checkpoint failed: %w", err), m.styles))
			} else {
				m.addMessage(msgcomp.NewUserMessage("Checkpoint created.", m.styles))
			}
		} else {
			m.addMessage(msgcomp.NewUserMessage("No git provider available.", m.styles))
		}
		m.setState(api.TurnIdle)

	case MCPListMsg:
		if msg.Tools == nil {
			m.addMessage(msgcomp.NewUserMessage("No MCP tools connected.", m.styles))
		} else if len(msg.Tools) == 0 {
			m.addMessage(msgcomp.NewUserMessage("No MCP tools available.", m.styles))
		} else {
			for _, t := range msg.Tools {
				m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("MCP tool: %s — %s", t.Name, t.Description), m.styles))
			}
		}
		m.setState(api.TurnIdle)

	case msgcomp.RenderInvalidateMsg:
		m.rebuildRenderedContent()

	case input.SendMsg:
		cmds = append(cmds, m.handleSend(msg.Content)...)

	case sidebar.SelectFileMsg:
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Selected file: %s", msg.Path), m.styles))
	}

	// Update focused child component for KeyMsg; all children for other messages
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		keyStr := keyMsg.String()
		isTab := keyStr == m.config.Keybindings.FocusNext || keyStr == m.config.Keybindings.FocusPrev
		if !isTab {
			switch m.focused {
			case focusInput:
				cmds = append(cmds, m.input.UpdateMsg(msg))
			case focusViewport:
				cmds = append(cmds, m.vp.UpdateMsg(msg))
			case focusSidebar:
				if m.sidebar.Visible() {
					cmds = append(cmds, m.sidebar.UpdateMsg(msg))
					m.syncInputCandidates()
				}
			}
		}
	} else {
		cmds = append(cmds, m.input.UpdateMsg(msg))
		cmds = append(cmds, m.vp.UpdateMsg(msg))
		cmds = append(cmds, m.sidebar.UpdateMsg(msg))
		m.syncInputCandidates()
	}

	// Update messages - pass KeyMsg through only when the viewport is focused
	// so message-level bindings (e.g. "r" to toggle raw markdown) are reachable.
	if _, ok := msg.(tea.KeyMsg); ok {
		if m.focused == focusViewport {
			for i := range m.messages {
				cmds = append(cmds, m.messages[i].UpdateMsg(msg))
			}
		}
	} else {
		for i := range m.messages {
			cmds = append(cmds, m.messages[i].UpdateMsg(msg))
		}
	}

	if m.rb.isDirty() {
		m.refreshViewport()
		m.rb.markClean()
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var mainContent strings.Builder
	mainContent.WriteString(m.vp.View())
	mainContent.WriteString("\n")
	mainContent.WriteString(m.input.View())
	mainContent.WriteString("\n")
	mainContent.WriteString(m.statusBar())

	view := mainContent.String()
	if m.sidebar.Visible() {
		view = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.sidebar.View(),
			view,
		)
	}

	if m.state == api.TurnWaitingApproval && m.approval.isActive() {
		view = m.renderApprovalDialog(view)
	}

	return view
}

// SetSession sets the current session.
func (m *Model) SetSession(s *api.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = s
}

// SetModelName sets the displayed model name.
func (m *Model) SetModelName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelName = name
}

// SetContextStats updates context usage stats.
func (m *Model) SetContextStats(used, max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contextUsed = used
	m.contextMax = max
}

// updateContextStats estimates token usage from the current message history
// using a chars/4 heuristic and updates the displayed stats.
func (m *Model) updateContextStats() {
	if m.contextMax <= 0 {
		return
	}
	totalChars := 0
	for _, msg := range m.messages {
		totalChars += len(msg.Content)
	}
	m.SetContextStats(totalChars/4, m.contextMax)
}

// SetToolCount updates the tool count.
func (m *Model) SetToolCount(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCount = n
}

// State returns the current turn state.
func (m *Model) State() api.TurnState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Model) setState(s api.TurnState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
}

// syncInputCandidates refreshes the input's file completion list from the
// sidebar's currently visible tree.
func (m *Model) syncInputCandidates() {
	m.input.SetFileCandidates(m.sidebar.VisiblePaths())
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Approval dialog takes precedence when waiting for approval
	if m.state == api.TurnWaitingApproval {
		switch msg.String() {
		case m.config.Keybindings.ApproveYes:
			if resp, ok := m.approval.approveCurrent(api.ApprovalYes); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveNo:
			if resp, ok := m.approval.approveCurrent(api.ApprovalNo); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveAlways:
			if resp, ok := m.approval.approveCurrent(api.ApprovalAlways); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		}
	}

	switch msg.String() {
	case m.config.Keybindings.Quit:
		cmds = append(cmds, tea.Quit)
	case m.config.Keybindings.Cancel:
		m.mu.Lock()
		state := m.state
		cancel := m.streamCancel
		m.mu.Unlock()
		if state == api.TurnThinking || state == api.TurnStreaming {
			if cancel != nil {
				cancel()
			}
			m.mu.Lock()
			m.streamCh = nil
			m.streamCancel = nil
			m.streamCanceled = true
			m.mu.Unlock()
			m.setState(api.TurnIdle)
		}
	case m.config.Keybindings.ToggleSidebar:
		m.sidebar.Toggle()
		m.updateLayout()
		if !m.sidebar.Visible() && m.focused == focusSidebar {
			m.focused = focusInput
			m.syncInputCandidates()
			if cmd := m.input.Focus(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case m.config.Keybindings.FocusNext:
		if cmd := m.cycleFocus(1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case m.config.Keybindings.FocusPrev:
		if cmd := m.cycleFocus(-1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case m.config.Keybindings.Yolo:
		if m.approvalModeSetter != nil {
			m.mu.Lock()
			if m.approvalMode == 1 {
				m.approvalMode = 2
			} else {
				m.approvalMode = 1
			}
			mode := m.approvalMode
			m.mu.Unlock()
			m.approvalModeSetter(mode)
		}
	}

	return cmds
}

func (m *Model) cycleFocus(delta int) tea.Cmd {
	components := []focusedComponent{focusInput, focusViewport}
	if m.sidebar.Visible() {
		components = []focusedComponent{focusInput, focusSidebar, focusViewport}
	}

	currentIdx := -1
	for i, c := range components {
		if c == m.focused {
			currentIdx = i
			break
		}
	}

	newIdx := currentIdx + delta
	for newIdx < 0 {
		newIdx += len(components)
	}
	newIdx = newIdx % len(components)

	m.focused = components[newIdx]

	if m.focused == focusInput {
		m.syncInputCandidates()
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *Model) handleMouseMsg(msg tea.MouseMsg) {
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return
	}

	l := m.layout()

	if m.sidebar.Visible() && msg.X < l.sbWidth {
		m.focused = focusSidebar
		return
	}

	if msg.Y >= l.vpHeight && msg.Y < l.statusY {
		m.focused = focusInput
		m.syncInputCandidates()
	} else if msg.Y < l.vpHeight {
		m.focused = focusViewport
	}
}

func (m *Model) handleSend(content string) []tea.Cmd {
	var cmds []tea.Cmd

	if strings.HasPrefix(content, "/") {
		cmd := m.handleCommand(content)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	// Allow sending from Idle or Error state; block only during active turns.
	if m.state == api.TurnThinking || m.state == api.TurnStreaming || m.state == api.TurnToolCalls || m.state == api.TurnWaitingApproval {
		return nil
	}

	m.toolCount = 0
	m.addMessage(msgcomp.NewUserMessage(content, m.styles))
	m.setState(api.TurnThinking)

	// If turn manager and session are wired, execute a real LLM turn.
	if m.turnManager != nil && m.session != nil {
		ctx, cancel := context.WithCancel(m.appCtx)
		m.mu.Lock()
		m.streamCancel = cancel
		m.streamCh = nil
		m.streamCanceled = false
		m.mu.Unlock()
		streamCh, err := m.turnManager.RunTurn(ctx, m.session.ID, content)
		if err != nil {
			m.addMessage(msgcomp.NewErrorMessage(err, m.styles))
			m.setState(api.TurnError)
			return cmds
		}
		m.mu.Lock()
		m.streamCh = streamCh
		m.mu.Unlock()
		cmds = append(cmds, m.readStreamChunk())
		return cmds
	}

	// Fallback for tests or when no services are wired.
	cmds = append(cmds, func() tea.Msg {
		return SendMessageMsg{Content: content}
	})
	return cmds
}

func (m *Model) handleSessions() []tea.Cmd {
	var cmds []tea.Cmd
	if m.store != nil && m.session != nil {
		sessions, err := m.store.ListSessions(m.appCtx, m.session.Path, 0)
		if err != nil {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("list sessions: %w", err), m.styles))
		} else if len(sessions) == 0 {
			m.addMessage(msgcomp.NewUserMessage("No sessions found.", m.styles))
		} else {
			for _, s := range sessions {
				m.addMessage(msgcomp.NewUserMessage(
					fmt.Sprintf("Session: %s (%s) — updated %s", s.ID, s.Path, s.UpdatedAt.Format("2006-01-02 15:04")),
					m.styles,
				))
			}
		}
	} else {
		m.addMessage(msgcomp.NewUserMessage("No sessions available.", m.styles))
	}
	m.setState(api.TurnIdle)
	return cmds
}

// readStreamChunk returns a command that reads the next event from the stream.
func (m *Model) readStreamChunk() tea.Cmd {
	ch := m.streamCh // capture at command creation time
	return func() tea.Msg {
		if ch == nil {
			return StreamChunkMsg{Chunk: api.StreamChunk{Done: true}}
		}
		event, ok := <-ch
		if !ok {
			return StreamChunkMsg{Chunk: api.StreamChunk{Done: true}}
		}
		switch event.Type {
		case api.TurnEventContent:
			return StreamChunkMsg{Chunk: api.StreamChunk{Content: event.Content}}
		case api.TurnEventDone:
			return StreamChunkMsg{Chunk: api.StreamChunk{Done: true, ToolCalls: event.ToolCalls}}
		case api.TurnEventError:
			return ErrorMsg{Err: event.Error}
		case api.TurnEventApprovalRequest:
			return ApprovalRequestMsg{Calls: event.ToolCalls, RequestID: event.RequestID}
		case api.TurnEventToolResult:
			return ToolResultMsg{Result: event.Result}
		default:
			return nil
		}
	}
}

func (m *Model) handleStreamChunk(chunk api.StreamChunk) []tea.Cmd {
	var cmds []tea.Cmd

	// Guard against stale buffered chunks after stream cancellation.
	if !chunk.Done && m.streamCh == nil && m.streamCanceled {
		return cmds
	}

	if chunk.Error != nil {
		m.addMessage(msgcomp.NewErrorMessage(chunk.Error, m.styles))
		m.setState(api.TurnError)
		m.streamCh = nil
		m.streamCanceled = true
		m.rb.setLastBlockStart(0)
		return cmds
	}

	if chunk.Done {
		if m.state != api.TurnError {
			m.setState(api.TurnIdle)
		}
		if lastMsg := m.lastAssistantMessage(); lastMsg != nil {
			lastMsg.SetStreaming(false)
		}
		// Rebuild renderedContent since the last assistant message may now be glamour-rendered
		m.rebuildRenderedContent()
		// Estimate token usage after turn completes.
		m.updateContextStats()
		if len(chunk.ToolCalls) > 0 {
			// Do not clear streamCh yet; more events may come from the turn manager.
			cmds = append(cmds, func() tea.Msg {
				return ToolCallMsg{Calls: chunk.ToolCalls}
			})
			return cmds
		}
		m.streamCh = nil
		return cmds
	}

	if m.state != api.TurnStreaming {
		m.setState(api.TurnStreaming)
	}

	// Find or create the last assistant message
	lastMsg := m.lastAssistantMessage()
	if lastMsg == nil {
		lastMsg = msgcomp.NewAssistantMessage("", m.styles)
		lastMsg.SetStreaming(true)
		m.messages = append(m.messages, lastMsg)
		m.rb.setLastBlockStart(m.rb.len())
		m.rb.updateLastBlock(lastMsg.View())
	} else if m.messages[len(m.messages)-1] != lastMsg {
		// Last assistant is not the most recent message; fall back to full rebuild
		lastMsg.AppendContent(chunk.Content)
		lastMsg.SetWidth(m.vpWidth())
		m.rebuildRenderedContent()
		cmds = append(cmds, m.readStreamChunk())
		return cmds
	} else if m.rb.lastBlockStart() == 0 {
		// After a full rebuild (e.g., resize), recompute the assistant's start position
		m.rb.setLastBlockStart(m.rb.len() - len(lastMsg.View()))
		if m.rb.lastBlockStart() < 0 {
			m.rb.setLastBlockStart(0)
		}
	}

	lastMsg.AppendContent(chunk.Content)
	lastMsg.SetWidth(m.vpWidth())

	// Incremental update: truncate back to lastBlockStart and re-render just the last message
	m.rb.updateLastBlock(lastMsg.View())

	// Continue polling for the next chunk
	cmds = append(cmds, m.readStreamChunk())

	return cmds
}

func (m *Model) handleToolCalls(calls []api.ToolCall) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 1)
	for _, call := range calls {
		m.addMessage(msgcomp.NewToolCallMessage(call, m.styles))
	}
	m.toolCount += len(calls)
	m.setState(api.TurnToolCalls)
	cmds = append(cmds, m.readStreamChunk())
	return cmds
}

func (m *Model) handleToolResult(result api.ToolResult) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 1)
	for _, msg := range m.messages {
		if msg.Type == msgcomp.TypeToolCall && msg.ToolCall.ID == result.CallID {
			msg.SetToolResult(result)
			break
		}
	}
	// Re-render the tool call message so the result is visible in the viewport.
	m.rebuildRenderedContent()
	cmds = append(cmds, m.readStreamChunk())
	return cmds
}

func (m *Model) handleApprovalResponse(resp ApprovalResponseMsg) []tea.Cmd {
	var cmds []tea.Cmd

	done, approvals, alwaysAll := m.approval.handleResponse(resp)
	if !done {
		return cmds
	}

	if alwaysAll && m.autoApproveSetter != nil {
		for _, call := range m.approval.pending() {
			m.autoApproveSetter(call.Name)
		}
	}

	reqID := m.approval.requestID()
	m.approval.clear()
	m.setState(api.TurnThinking)
	cmds = append(cmds, func() tea.Msg {
		if m.turnManager != nil && m.session != nil {
			_ = m.turnManager.ResumeWithApproval(m.appCtx, m.session.ID, reqID, approvals)
		}
		return StateChangeMsg{State: api.TurnThinking}
	})
	cmds = append(cmds, m.readStreamChunk())
	return cmds
}

func (m *Model) addMessage(msg *msgcomp.Message) {
	msg.SetWidth(m.vpWidth())
	m.messages = append(m.messages, msg)
	m.rb.appendBlock(msg.View())
}

func (m *Model) clearMessages() {
	m.messages = make([]*msgcomp.Message, 0)
	m.rb.reset()
	m.vp.SetContent("")
}

func (m *Model) rebuildRenderedContent() {
	blocks := make([]string, len(m.messages))
	for i, msg := range m.messages {
		blocks[i] = msg.View()
	}
	m.rb.rebuild(blocks)
	m.rebuildCount++
}

func (m *Model) lastAssistantMessage() *msgcomp.Message {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == msgcomp.TypeAssistant {
			return m.messages[i]
		}
	}
	return nil
}

func (m *Model) refreshViewport() {
	m.vp.SetContent(m.rb.String())
}

func (m *Model) renderApprovalDialog(background string) string {
	call, ok := m.approval.currentCall()
	if !ok {
		return background
	}
	var b strings.Builder
	b.WriteString("Tool call requires approval\n\n")
	fmt.Fprintf(&b, "Tool: %s\n", call.Name)
	if diff := toolCallDiff(call, m.session.Path); diff != "" {
		fmt.Fprintf(&b, "\n%s\n", diff)
	} else {
		fmt.Fprintf(&b, "Arguments: %s\n", call.Arguments)
	}
	fmt.Fprintf(&b, "\n[%s] yes  [%s] no  [%s] always", m.config.Keybindings.ApproveYes, m.config.Keybindings.ApproveNo, m.config.Keybindings.ApproveAlways)

	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

// overlayDialog composites a dialog box over a background, centering it both
// horizontally and vertically. The background is normalized to exactly width x
// height cells before the dialog is painted on top, so the rendered output is
// stable even on narrow terminals or when the dialog is larger than the
// background. Wide runes (CJK/emoji) are handled via ansi.Cut.
//
// The overlay is implemented with lipgloss v2's Canvas and Layer compositor
// instead of hand-rolled ANSI string splicing.
func overlayDialog(background string, dialog string, width int, height int) string {
	bgLines := strings.Split(background, "\n")

	// Normalize the background to exactly height lines.
	if len(bgLines) < height {
		bgLines = append(bgLines, make([]string, height-len(bgLines))...)
	}
	bgLines = bgLines[:height]

	// Normalize each line to exactly width cells.
	for i, line := range bgLines {
		lineWidth := ansi.StringWidth(line)
		switch {
		case lineWidth > width:
			bgLines[i] = ansi.Cut(line, 0, width)
		case lineWidth < width:
			bgLines[i] = line + strings.Repeat(" ", width-lineWidth)
		}
	}

	dialogHeight := gloss.Height(dialog)
	dialogWidth := gloss.Width(dialog)

	startY := (height - dialogHeight) / 2
	if startY < 0 {
		startY = 0
	}
	startX := (width - dialogWidth) / 2
	if startX < 0 {
		startX = 0
	}

	// Clamp the dialog line so the rendered output never exceeds the
	// requested width, even on very narrow terminals.
	maxDialogWidth := width - startX
	dialogLines := strings.Split(dialog, "\n")
	for i, dLine := range dialogLines {
		if ansi.StringWidth(dLine) > maxDialogWidth {
			dialogLines[i] = ansi.Cut(dLine, 0, maxDialogWidth)
		}
	}
	dialog = strings.Join(dialogLines, "\n")

	comp := gloss.NewCompositor(
		gloss.NewLayer(strings.Join(bgLines, "\n")),
		gloss.NewLayer(dialog).X(startX).Y(startY).Z(1),
	)
	rendered := gloss.NewCanvas(width, height).Compose(comp).Render()

	// Canvas.Render trims trailing whitespace. Re-normalize each line to the
	// requested width x height so callers get a stable, predictable rectangle.
	return normalizeRect(rendered, width, height)
}

// normalizeRect pads or truncates a rendered string so that every line has
// exactly width cells and the output contains exactly height lines.
func normalizeRect(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	lines = lines[:height]
	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		switch {
		case lineWidth > width:
			lines[i] = ansi.Cut(line, 0, width)
		case lineWidth < width:
			lines[i] = line + strings.Repeat(" ", width-lineWidth)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) statusBar() string {
	m.mu.RLock()
	state := m.state
	modelName := m.modelName
	contextUsed := m.contextUsed
	contextMax := m.contextMax
	toolCount := m.toolCount
	approvalMode := m.approvalMode
	m.mu.RUnlock()

	stateStr := state.ShortString()

	parts := make([]string, 0, 5)
	parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf(" %s ", modelName)))
	parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf("[%s]", stateStr)))

	if approvalMode == 2 {
		parts = append(parts, m.styles.StatusBar.Render(" YOLO "))
	}

	if m.config.UI.ShowTokenCount && contextMax > 0 {
		pct := float64(contextUsed) / float64(contextMax) * 100
		parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf("ctx: %.0f%%", pct)))
	}

	if toolCount > 0 {
		parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf("tools: %d", toolCount)))
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	return m.styles.StatusBar.Width(m.contentWidth()).Render(bar)
}

func (m *Model) updateLayout() {
	l := m.layout()
	m.applyLayoutSizes(l)

	// If the layout geometry has not changed, the transcript is still valid.
	if l.eq(m.lastLayout) {
		return
	}

	for _, msg := range m.messages {
		msg.SetWidth(l.vpWidth)
	}
	m.rebuildRenderedContent()
	m.lastLayout = l
}

// applyLayoutSizes updates child component sizes without rebuilding the transcript.
func (m *Model) applyLayoutSizes(l layoutRect) {
	m.sidebar.SetSize(l.sbWidth, m.height-1)
	m.vp.SetSize(l.contentWidth, l.vpHeight)
	m.input.SetWidth(l.contentWidth)
}

func (m *Model) contentWidth() int {
	return m.layout().contentWidth
}

func (m *Model) vpWidth() int {
	return m.layout().vpWidth
}
