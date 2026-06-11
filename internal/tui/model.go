// Package tui provides the Terminal UI layer for kimi-lite using Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// StreamChunkMsg carries a chunk from the LLM stream.
type StreamChunkMsg struct {
	Chunk api.StreamChunk
}

// ToolCallMsg signals that tool calls were received.
type ToolCallMsg struct {
	Calls []api.ToolCall
}

// ToolResultMsg carries a tool execution result.
type ToolResultMsg struct {
	Result api.ToolResult
}

// ErrorMsg carries an error to display.
type ErrorMsg struct {
	Err error
}

// StateChangeMsg signals a turn state change.
type StateChangeMsg struct {
	State api.TurnState
}

// ApprovalRequestMsg requests user approval for tool calls.
type ApprovalRequestMsg struct {
	Calls     []api.ToolCall
	RequestID int64
}

// ApprovalResponseMsg carries the user's approval decision.
type ApprovalResponseMsg struct {
	Decision api.ApprovalDecision
	CallID   string
}

// SendMessageMsg is emitted by the root model to the app layer.
type SendMessageMsg struct {
	Content string
}

// CompactMsg signals the app to compact the session.
type CompactMsg struct{}

// ClearMsg signals the app to clear messages.
type ClearMsg struct{}

// SessionsMsg signals the app to list sessions.
type SessionsMsg struct{}

// GoalMsg signals the app to set a goal.
type GoalMsg struct {
	Content string
}

// BTWMsg signals a btw message.
type BTWMsg struct {
	Content string
}

// CheckpointMsg signals the app to create a checkpoint.
type CheckpointMsg struct{}

// focusedComponent tracks which TUI component has keyboard focus.
type focusedComponent int

const (
	focusInput focusedComponent = iota
	focusSidebar
	focusViewport
)

const (
	statusHeight         = 1
	minContentWidth      = 20
	minViewportHeight    = 5
	viewportWidthPadding = 4
)

// inputHeight returns the current rendered height of the input component.
func (m *Model) inputHeight() int {
	h := m.input.Height()
	if h < 3 {
		return 3
	}
	return h
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

	pendingApprovals  []api.ToolCall
	approvalRequestID int64
	approvalIndex     int
	approvalDecisions map[string]api.ApprovalDecision

	// Service references (optional, wired by app layer)
	turnManager    turnManager
	sessionManager sessionManager
	compressor     compressor
	gitProvider    api.GitProvider
	mcpClient      api.MCPClient
	store          api.Store

	// Streaming state
	streamCh     <-chan api.TurnEvent
	streamCancel context.CancelFunc

	// Focus management
	focused focusedComponent

	// Context propagation
	appCtx context.Context

	// Cached rendered content for O(1) viewport updates
	renderedContent        strings.Builder
	lastAssistantRenderPos int // byte position in renderedContent where the last assistant message starts

	mu sync.RWMutex
}

// New creates the root TUI model.
func New(cfg *api.Config, session *api.Session, appCtx context.Context) (*Model, error) {
	st := styles.New(cfg.UI.Theme)

	inp := input.New(st, input.ConfigurableKeyMap(cfg.Keybindings))
	vp := viewport.New(st)

	sb, err := sidebar.New(st, session.Path)
	if err != nil {
		return nil, fmt.Errorf("create sidebar: %w", err)
	}

	m := &Model{
		styles:        st,
		config:        cfg,
		input:         inp,
		vp:            vp,
		sidebar:       sb,
		messages:      make([]*msgcomp.Message, 0),
		state:         api.TurnIdle,
		session:       session,
		contextMax:    0,
		modelName:     cfg.LLM.Model,
		approvalIndex: -1,
		focused:       focusInput,
		appCtx:        appCtx,
	}
	m.updateLayout()
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
		cmds = append(cmds, m.handleKeyMsg(msg)...)
	case tea.MouseMsg:
		m.handleMouseMsg(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()

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

	case ApprovalRequestMsg:
		m.pendingApprovals = msg.Calls
		m.approvalRequestID = msg.RequestID
		m.approvalIndex = 0
		m.setState(api.TurnWaitingApproval)

	case ApprovalResponseMsg:
		cmds = append(cmds, m.handleApprovalResponse(msg)...)

	case input.SendMsg:
		cmds = append(cmds, m.handleSend(msg.Content)...)

	case sidebar.SelectFileMsg:
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Selected file: %s", msg.Path), m.styles))
	}

	// Update focused child component for KeyMsg; all children for other messages
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		keyStr := keyMsg.String()
		isTab := keyStr == "tab" || keyStr == "shift+tab"
		if !isTab {
			switch m.focused {
			case focusInput:
				inpModel, cmd := m.input.Update(msg)
				switch im := inpModel.(type) {
				case *input.Model:
					m.input = im
				}
				cmds = append(cmds, cmd)
			case focusViewport:
				vpModel, cmd := m.vp.Update(msg)
				switch vm := vpModel.(type) {
				case *viewport.Model:
					m.vp = vm
				}
				cmds = append(cmds, cmd)
			case focusSidebar:
				if m.sidebar.Visible() {
					sbModel, cmd := m.sidebar.Update(msg)
					switch sm := sbModel.(type) {
					case *sidebar.Model:
						m.sidebar = sm
					}
					cmds = append(cmds, cmd)
				}
			}
		}
	} else {
		var cmd tea.Cmd
		inpModel, cmd := m.input.Update(msg)
		switch im := inpModel.(type) {
		case *input.Model:
			m.input = im
		}
		cmds = append(cmds, cmd)

		var vpModel tea.Model
		vpModel, cmd = m.vp.Update(msg)
		switch vm := vpModel.(type) {
		case *viewport.Model:
			m.vp = vm
		}
		cmds = append(cmds, cmd)

		var sbModel tea.Model
		sbModel, cmd = m.sidebar.Update(msg)
		switch sm := sbModel.(type) {
		case *sidebar.Model:
			m.sidebar = sm
		}
		cmds = append(cmds, cmd)
	}

	// Update messages - do not pass KeyMsg to message components (mouse only)
	if _, ok := msg.(tea.KeyMsg); !ok {
		for i, msgModel := range m.messages {
			updated, msgCmd := msgModel.Update(msg)
			switch um := updated.(type) {
			case *msgcomp.Message:
				m.messages[i] = um
			}
			cmds = append(cmds, msgCmd)
		}
	}

	m.refreshViewport()

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

	if m.state == api.TurnWaitingApproval && m.approvalIndex >= 0 && m.approvalIndex < len(m.pendingApprovals) {
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

func (m *Model) handleKeyMsg(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Approval dialog takes precedence when waiting for approval
	if m.state == api.TurnWaitingApproval {
		switch msg.String() {
		case "y":
			cmds = append(cmds, m.approveCurrent(api.ApprovalYes))
			return cmds
		case "n":
			cmds = append(cmds, m.approveCurrent(api.ApprovalNo))
			return cmds
		case "a":
			cmds = append(cmds, m.approveCurrent(api.ApprovalAlways))
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
			m.mu.Unlock()
			m.setState(api.TurnIdle)
		}
	case "ctrl+b":
		m.sidebar.Toggle()
		m.updateLayout()
		if !m.sidebar.Visible() && m.focused == focusSidebar {
			m.focused = focusInput
			if cmd := m.input.Focus(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case "tab":
		if cmd := m.cycleFocus(1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case "shift+tab":
		if cmd := m.cycleFocus(-1); cmd != nil {
			cmds = append(cmds, cmd)
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
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *Model) handleMouseMsg(msg tea.MouseMsg) {
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return
	}

	sbWidth := 0
	if m.sidebar.Visible() {
		sbWidth = m.sidebar.Width()
	}

	if m.sidebar.Visible() && msg.X < sbWidth {
		m.focused = focusSidebar
		return
	}

	vpHeight := m.height - statusHeight - m.inputHeight()

	if msg.Y >= vpHeight && msg.Y < vpHeight+m.inputHeight() {
		m.focused = focusInput
	} else if msg.Y < vpHeight {
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
		default:
			return nil
		}
	}
}

func (m *Model) handleCommand(content string) tea.Cmd {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	args := strings.TrimSpace(strings.TrimPrefix(content, cmd))

	switch cmd {
	case "/compact":
		m.addMessage(msgcomp.NewUserMessage("Compacting session...", m.styles))
		return func() tea.Msg {
			if m.compressor != nil && m.store != nil && m.session != nil {
				_, err := m.compressor.Compact(m.appCtx, m.store, m.session.ID, 2)
				if err != nil {
					return ErrorMsg{Err: err}
				}
				return StateChangeMsg{State: api.TurnIdle}
			}
			return CompactMsg{}
		}
	case "/clear":
		if m.state == api.TurnStreaming || m.state == api.TurnThinking {
			return nil
		}
		m.clearMessages()
		if m.sessionManager != nil && m.session != nil {
			_ = m.sessionManager.ClearMessages(m.appCtx, m.session.ID)
		}
		return func() tea.Msg { return ClearMsg{} }
	case "/sessions":
		m.addMessage(msgcomp.NewUserMessage("Listing sessions...", m.styles))
		return func() tea.Msg { return SessionsMsg{} }
	case "/goal":
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Goal set: %s", args), m.styles))
		return func() tea.Msg { return GoalMsg{Content: args} }
	case "/btw":
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("BTW: %s", args), m.styles))
		return func() tea.Msg { return BTWMsg{Content: args} }
	case "/checkpoint":
		m.addMessage(msgcomp.NewUserMessage("Creating checkpoint...", m.styles))
		return func() tea.Msg { return CheckpointMsg{} }
	default:
		m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("unknown command: %s", cmd), m.styles))
		return nil
	}
}

func (m *Model) handleStreamChunk(chunk api.StreamChunk) []tea.Cmd {
	var cmds []tea.Cmd

	if chunk.Error != nil {
		m.addMessage(msgcomp.NewErrorMessage(chunk.Error, m.styles))
		m.setState(api.TurnError)
		m.streamCh = nil
		m.lastAssistantRenderPos = 0
		return cmds
	}

	if chunk.Done {
		if m.state != api.TurnError {
			m.setState(api.TurnIdle)
		}
		m.streamCh = nil
		if lastMsg := m.lastAssistantMessage(); lastMsg != nil {
			lastMsg.SetStreaming(false)
		}
		// Rebuild renderedContent since the last assistant message may now be glamour-rendered
		m.rebuildRenderedContent()
		if len(chunk.ToolCalls) > 0 {
			cmds = append(cmds, func() tea.Msg {
				return ToolCallMsg{Calls: chunk.ToolCalls}
			})
		}
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
		m.lastAssistantRenderPos = m.renderedContent.Len()
	} else if m.messages[len(m.messages)-1] != lastMsg {
		// Last assistant is not the most recent message; fall back to full rebuild
		lastMsg.AppendContent(chunk.Content)
		lastMsg.SetWidth(m.vpWidth())
		m.rebuildRenderedContent()
		cmds = append(cmds, m.readStreamChunk())
		return cmds
	} else if m.lastAssistantRenderPos == 0 {
		// After a full rebuild (e.g., resize), recompute the assistant's start position
		m.lastAssistantRenderPos = m.renderedContent.Len() - len(lastMsg.View())
		if m.lastAssistantRenderPos < 0 {
			m.lastAssistantRenderPos = 0
		}
	}

	lastMsg.AppendContent(chunk.Content)
	lastMsg.SetWidth(m.vpWidth())

	// Incremental update: truncate back to lastAssistantRenderPos and re-render just the last message
	current := m.renderedContent.String()
	if len(current) > m.lastAssistantRenderPos {
		truncated := current[:m.lastAssistantRenderPos]
		m.renderedContent.Reset()
		m.renderedContent.WriteString(truncated)
	}
	m.renderedContent.WriteString(lastMsg.View())

	// Continue polling for the next chunk
	cmds = append(cmds, m.readStreamChunk())

	return cmds
}

func (m *Model) handleToolCalls(calls []api.ToolCall) []tea.Cmd {
	var cmds []tea.Cmd
	for _, call := range calls {
		m.addMessage(msgcomp.NewToolCallMessage(call, m.styles))
	}
	m.toolCount += len(calls)
	m.setState(api.TurnToolCalls)
	return cmds
}

func (m *Model) handleToolResult(result api.ToolResult) []tea.Cmd {
	var cmds []tea.Cmd
	for _, msg := range m.messages {
		if msg.Type == msgcomp.TypeToolCall && msg.ToolCall.ID == result.CallID {
			msg.SetToolResult(result)
			break
		}
	}
	return cmds
}

func (m *Model) handleApprovalResponse(resp ApprovalResponseMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if m.approvalDecisions == nil {
		m.approvalDecisions = make(map[string]api.ApprovalDecision)
	}

	if resp.Decision == api.ApprovalAlways {
		approvals := make(map[string]api.ApprovalDecision)
		for _, call := range m.pendingApprovals {
			approvals[call.ID] = api.ApprovalYes
		}
		m.pendingApprovals = nil
		m.approvalIndex = -1
		m.approvalDecisions = nil
		m.setState(api.TurnThinking)
		cmds = append(cmds, m.resumeWithApprovals(approvals))
		return cmds
	}

	m.approvalDecisions[resp.CallID] = resp.Decision
	m.approvalIndex++

	if m.approvalIndex >= len(m.pendingApprovals) {
		approvals := make(map[string]api.ApprovalDecision)
		for _, call := range m.pendingApprovals {
			if d, ok := m.approvalDecisions[call.ID]; ok {
				approvals[call.ID] = d
			} else {
				approvals[call.ID] = api.ApprovalNo
			}
		}
		m.pendingApprovals = nil
		m.approvalIndex = -1
		m.approvalDecisions = nil
		m.setState(api.TurnThinking)
		cmds = append(cmds, m.resumeWithApprovals(approvals))
	}

	return cmds
}

func (m *Model) approveCurrent(decision api.ApprovalDecision) tea.Cmd {
	if m.approvalIndex < 0 || m.approvalIndex >= len(m.pendingApprovals) {
		return nil
	}

	call := m.pendingApprovals[m.approvalIndex]
	return func() tea.Msg {
		return ApprovalResponseMsg{Decision: decision, CallID: call.ID}
	}
}

func (m *Model) resumeWithApprovals(approvals map[string]api.ApprovalDecision) tea.Cmd {
	// Clear state in main thread before returning async command.
	reqID := m.approvalRequestID
	m.pendingApprovals = nil
	m.approvalRequestID = 0
	m.approvalIndex = -1
	m.approvalDecisions = nil
	return func() tea.Msg {
		if m.turnManager != nil && m.session != nil {
			_ = m.turnManager.ResumeWithApproval(m.appCtx, m.session.ID, reqID, approvals)
		}
		return StateChangeMsg{State: api.TurnThinking}
	}
}

func (m *Model) addMessage(msg *msgcomp.Message) {
	msg.SetWidth(m.vpWidth())
	m.messages = append(m.messages, msg)
	m.renderedContent.WriteString(msg.View())
	m.renderedContent.WriteString("\n\n")
}

func (m *Model) clearMessages() {
	m.messages = make([]*msgcomp.Message, 0)
	m.renderedContent.Reset()
	m.lastAssistantRenderPos = 0
	m.vp.SetContent("")
}

func (m *Model) rebuildRenderedContent() {
	m.renderedContent.Reset()
	for i, msg := range m.messages {
		m.renderedContent.WriteString(msg.View())
		if i < len(m.messages)-1 {
			m.renderedContent.WriteString("\n\n")
		}
	}
	m.lastAssistantRenderPos = 0
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
	m.vp.SetContent(m.renderedContent.String())
}

func (m *Model) renderApprovalDialog(background string) string {
	if m.approvalIndex < 0 || m.approvalIndex >= len(m.pendingApprovals) {
		return background
	}

	call := m.pendingApprovals[m.approvalIndex]
	var b strings.Builder
	b.WriteString("Tool call requires approval\n\n")
	b.WriteString(fmt.Sprintf("Tool: %s\n", call.Name))
	b.WriteString(fmt.Sprintf("Arguments: %s\n", call.Arguments))
	b.WriteString("\n[y] yes  [n] no  [a] always")

	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

func overlayDialog(background string, dialog string, width int, height int) string {
	bgLines := strings.Split(background, "\n")
	dialogHeight := lipgloss.Height(dialog)
	dialogWidth := lipgloss.Width(dialog)

	startY := (height - dialogHeight) / 2
	startX := (width - dialogWidth) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	endX := startX + dialogWidth

	dialogLines := strings.Split(dialog, "\n")
	for i, dLine := range dialogLines {
		y := startY + i
		if y < 0 || y >= len(bgLines) {
			continue
		}

		bLine := bgLines[y]
		bWidth := ansi.StringWidth(bLine)

		var leftPart string
		if startX > 0 {
			if startX >= bWidth {
				leftPart = bLine + strings.Repeat(" ", startX-bWidth)
			} else {
				leftPart = ansi.Cut(bLine, 0, startX)
			}
		}

		var rightPart string
		if endX < bWidth {
			rightPart = ansi.Cut(bLine, endX, bWidth)
		}

		bgLines[y] = leftPart + dLine + rightPart
	}

	return strings.Join(bgLines, "\n")
}

func (m *Model) statusBar() string {
	m.mu.RLock()
	state := m.state
	modelName := m.modelName
	contextUsed := m.contextUsed
	contextMax := m.contextMax
	toolCount := m.toolCount
	m.mu.RUnlock()

	var stateStr string
	switch state {
	case api.TurnIdle:
		stateStr = "idle"
	case api.TurnThinking:
		stateStr = "thinking"
	case api.TurnStreaming:
		stateStr = "streaming"
	case api.TurnToolCalls:
		stateStr = "tools"
	case api.TurnWaitingApproval:
		stateStr = "approval"
	case api.TurnError:
		stateStr = "error"
	}

	var parts []string
	parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf(" %s ", modelName)))
	parts = append(parts, m.styles.StatusBar.Render(fmt.Sprintf("[%s]", stateStr)))

	if contextMax > 0 {
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
	sbWidth := 0
	if m.sidebar.Visible() {
		sbWidth = m.sidebar.Width()
	}
	contentWidth := m.width - sbWidth
	if contentWidth < minContentWidth {
		contentWidth = minContentWidth
	}

	m.sidebar.SetSize(sbWidth, m.height-1)

	vpHeight := m.height - statusHeight - m.inputHeight()
	if vpHeight < minViewportHeight {
		vpHeight = minViewportHeight
	}
	m.vp.SetSize(contentWidth, vpHeight)
	m.input.SetWidth(contentWidth)

	// Invalidate renderedContent on resize and rebuild with updated widths
	vpW := m.vpWidth()
	for _, msg := range m.messages {
		msg.SetWidth(vpW)
	}
	m.rebuildRenderedContent()
}

func (m *Model) contentWidth() int {
	sbWidth := 0
	if m.sidebar.Visible() {
		sbWidth = m.sidebar.Width()
	}
	w := m.width - sbWidth
	if w < minContentWidth {
		w = minContentWidth
	}
	return w
}

func (m *Model) vpWidth() int {
	return m.contentWidth() - viewportWidthPadding
}
