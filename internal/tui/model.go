// Package tui provides the Terminal UI layer for kimi-lite using Bubble Tea.
package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/activity"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/footer"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/help"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/mentions"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/sessions"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/viewport"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/welcome"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// focusedComponent tracks which TUI component has keyboard focus.
type focusedComponent int

const (
	focusInput focusedComponent = iota
	focusViewport
)

const (
	statusHeight      = 2
	minContentWidth   = 20
	minViewportHeight = 5
	// viewportWidthPadding accounts for viewport border + content padding.
	viewportWidthPadding = 4
	minInputHeight       = 3

	resizeDebounce = 80 * time.Millisecond

	// approvalModeAuto and approvalModeYolo mirror core approval modes in the
	// integer representation used by the TUI's approval-mode callback.
	approvalModeAuto = int(core.ModeAuto)
	approvalModeYolo = int(core.ModeYolo)
)

// inputHeight returns the current rendered height of the input component.
func (m *Model) inputHeight() int {
	h := m.input.Height()
	if h < minInputHeight {
		return minInputHeight
	}
	return h
}

// welcomeHeight returns the rendered height of the welcome panel, or 0 when
// the transcript already contains messages.
func (m *Model) welcomeHeight() int {
	if len(m.messages) > 0 {
		return 0
	}
	return lipgloss.Height(m.welcome.View())
}

// activityHeight returns the rendered height of the activity panel, or 0 when
// the turn is not actively thinking, streaming, or running tools.
func (m *Model) activityHeight() int {
	return m.activity.Height()
}

// layoutRect holds computed geometry for a single frame.
type layoutRect struct {
	contentWidth int
	vpWidth      int
	vpHeight     int
	inputHeight  int
	statusY      int
}

// eq reports whether two layouts have identical geometry.
func (r layoutRect) eq(o layoutRect) bool {
	return r.contentWidth == o.contentWidth &&
		r.vpWidth == o.vpWidth &&
		r.vpHeight == o.vpHeight &&
		r.inputHeight == o.inputHeight &&
		r.statusY == o.statusY
}

// layout computes all geometry once, folding in min-content/min-viewport clamps.
func (m *Model) layout() layoutRect {
	contentWidth := m.width
	if contentWidth < minContentWidth {
		contentWidth = minContentWidth
	}
	m.welcome.SetSize(contentWidth)
	m.updateWelcomeData()
	m.activity.SetSize(contentWidth)
	m.updateActivity()
	welcomeHeight := m.welcomeHeight()
	inputHeight := m.inputHeight()
	activityHeight := m.activityHeight()
	vpHeight := m.height - statusHeight - inputHeight - welcomeHeight - activityHeight
	if vpHeight < minViewportHeight {
		vpHeight = minViewportHeight
	}
	statusY := welcomeHeight + vpHeight + inputHeight + activityHeight
	if statusY > m.height {
		statusY = m.height
	}
	return layoutRect{
		contentWidth: contentWidth,
		vpWidth:      contentWidth - viewportWidthPadding,
		vpHeight:     vpHeight,
		inputHeight:  inputHeight,
		statusY:      statusY,
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
	Resume(ctx context.Context, id string) (*api.Session, error)
	List(ctx context.Context, path string) ([]api.Session, error)
	ListAll(ctx context.Context, limit int) ([]api.Session, error)
	ClearMessages(ctx context.Context, id string) error
	Rename(ctx context.Context, id string, name string) error
	Fork(ctx context.Context, sourceID string, name string) (*api.Session, error)
}

// compressor is the interface needed from core.ContextCompressor.
type compressor interface {
	Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error)
}

// Model is the root Bubble Tea model composing all child models.
// It displays transient status messages for long-running tools.
type Model struct {
	styles *styles.Styles
	config *api.Config

	input    *input.Model
	vp       *viewport.Model
	messages []*msgcomp.Message
	footer   *footer.Model
	welcome  *welcome.Model
	activity *activity.Model

	mentionProvider mentions.Provider

	state       api.TurnState
	session     *api.Session
	modelName   string
	contextUsed int
	contextMax  int
	toolCount   int
	statusText  string

	gitBranch string
	gitDirty  bool
	gitAhead  int
	gitBehind int

	width  int
	height int

	// approvalController owns the pending tool-call approval state machine.
	approval *approvalController

	// sessionPicker is the modal session-selection overlay.
	sessionPicker *sessions.Picker

	// helpPanel is the /help overlay.
	helpPanel *help.Model
	showHelp  bool

	// approvalFullscreen shows a temporary fullscreen diff preview for the
	// pending approval call.
	approvalFullscreen  bool
	approvalDiffContent string

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
	approvalMode       int // approvalModeAuto by default; toggles to approvalModeYolo

	// protectedPaths are additional paths blocked by diff previews (mirrors
	// BuiltInToolExecutor.protectedPaths).
	protectedPaths []string

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
	if _, err := os.Stat(session.Path); err != nil {
		return nil, fmt.Errorf("session path %q: %w", session.Path, err)
	}

	st := styles.New(cfg.UI.Theme)

	inp := input.New(st, input.ConfigurableKeyMap(cfg.Keybindings), cfg.Session.MaxHistory)
	inp.SetEditor(cfg.UI.Editor)
	inp.SetContext(appCtx)
	inp.SetSlashCommands(input.DefaultSlashCommands)
	vp := viewport.New(st)
	ft := footer.New(st)
	wc := welcome.New(st)
	ac := activity.New(st)
	hp := help.New(st)

	mp := &mentions.FileWalker{MaxDepth: 3}
	m := &Model{
		styles:          st,
		config:          cfg,
		input:           inp,
		vp:              vp,
		footer:          ft,
		welcome:         wc,
		activity:        ac,
		helpPanel:       hp,
		messages:        make([]*msgcomp.Message, 0),
		state:           api.TurnIdle,
		session:         session,
		contextMax:      0,
		modelName:       cfg.LLM.Model,
		focused:         focusInput,
		appCtx:          appCtx,
		rb:              newRenderBuffer(),
		approval:        newApprovalController(),
		approvalMode:    approvalModeAuto,
		mentionProvider: mp,
	}
	inp.SetCandidateFunc(func() []string {
		m.mu.RLock()
		provider := m.mentionProvider
		path := ""
		if m.session != nil {
			path = m.session.Path
		}
		m.mu.RUnlock()
		if provider == nil {
			return nil
		}
		cands, _ := provider.Candidates(path)
		return cands
	})
	m.updateLayout()
	m.refreshFileCandidates()
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

// SetProtectedPaths sets additional paths that must be blocked by diff previews.
func (m *Model) SetProtectedPaths(paths []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protectedPaths = append([]string(nil), paths...)
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

func (m *Model) helpData() help.Data {
	return help.Data{
		Shortcuts: []help.Shortcut{
			{Keys: "enter", Description: "Send message"},
			{Keys: "alt+enter", Description: "Insert newline"},
			{Keys: "tab / shift+tab", Description: "Switch focus"},
			{Keys: "ctrl+g", Description: "External editor"},
			{Keys: "ctrl+y", Description: "Toggle yolo mode"},
			{Keys: "r", Description: "Toggle raw markdown (viewport focus)"},
			{Keys: "enter", Description: "Expand/collapse tool call"},
		},
		Commands: []help.SlashCommand{
			{Name: "/compact", Description: "Summarize older messages"},
			{Name: "/clear", Description: "Clear transcript"},
			{Name: "/sessions", Description: "Switch session"},
			{Name: "/checkpoint", Description: "Create git checkpoint"},
			{Name: "/diff", Description: "Show git diff"},
			{Name: "/mcp", Description: "List MCP tools"},
			{Name: "/title", Description: "Rename session"},
			{Name: "/fork", Description: "Fork session"},
			{Name: "/help", Description: "Show this help"},
		},
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Init(),
		m.vp.Init(),
		m.footer.Init(),
		m.activity.Init(),
		m.scheduleGitRefreshCmd(),
	)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.sessionPicker != nil {
			done, selected := m.sessionPicker.Update(msg)
			if selected {
				id := m.sessionPicker.Selected().ID
				m.sessionPicker = nil
				if id != "" {
					cmds = append(cmds, m.resumeSessionCmd(id))
				}
				return m, tea.Batch(cmds...)
			}
			if done {
				m.sessionPicker = nil
			}
			return m, nil
		}
		// Let the input component consume completion keys while a popup is open.
		if !m.input.Completing() {
			cmds = append(cmds, m.handleKeyMsg(msg)...)
		}
	case tea.MouseReleaseMsg:
		m.handleMouseMsg(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.sessionPicker != nil {
			m.sessionPicker.SetSize(m.width, m.height)
		}
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
		m.statusText = ""

	case StatusMsg:
		m.statusText = msg.Text

	case StateChangeMsg:
		m.setState(msg.State)

	case CompactResultMsg:
		if msg.Count == 0 {
			m.addMessage(msgcomp.NewUserMessage("Nothing to compact", m.styles))
		} else if msg.Summary != "" {
			m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Compacted %d messages. Summary:\n%s", msg.Count, msg.Summary), m.styles))
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
		if msg.Session == nil {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("fork session returned no session"), m.styles))
			m.setState(api.TurnError)
			break
		}
		m.session = msg.Session
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Forked to session %q (%s).", m.session.Name, m.session.ID), m.styles))
		m.setState(api.TurnIdle)

	case ApprovalRequestMsg:
		m.approvalStartRequest(msg.Calls, msg.RequestID)
		m.setState(api.TurnWaitingApproval)
		cmds = append(cmds, m.readStreamChunk())

	case ApprovalResponseMsg:
		cmds = append(cmds, m.handleApprovalResponse(msg)...)

	case ApprovalDiffMsg:
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Diff preview for %s:\n%s", msg.CallID, msg.Diff), m.styles))
		m.setState(api.TurnWaitingApproval)

	case SessionsMsg:
		cmds = append(cmds, m.listSessionsCmd())

	case SessionsResultMsg:
		if msg.Err != nil {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("list sessions: %w", msg.Err), m.styles))
		} else if len(msg.Sessions) == 0 {
			m.addMessage(msgcomp.NewUserMessage("No sessions found.", m.styles))
		} else {
			var path string
			if m.session != nil {
				path = m.session.Path
			}
			m.sessionPicker = sessions.NewPicker(msg.Sessions, path, m.width, m.height)
		}

	case SessionSelectedMsg:
		if msg.Err != nil {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("resume session: %w", msg.Err), m.styles))
		} else if msg.Session != nil {
			m.mu.Lock()
			m.session = msg.Session
			m.messages = nil
			m.rb = newRenderBuffer()
			m.mu.Unlock()
			m.refreshFileCandidates()
			for _, msg := range msg.Session.Messages {
				m.appendSessionMessage(msg)
			}
			m.updateLayout()
			m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Resumed session %s (%s)", msg.Session.ID, msg.Session.Path), m.styles))
		}
		m.setState(api.TurnIdle)

	case CheckpointMsg:
		cmds = append(cmds, m.checkpointCmd())

	case CheckpointResultMsg:
		if msg.Err != nil {
			m.addMessage(msgcomp.NewErrorMessage(msg.Err, m.styles))
		} else {
			m.addMessage(msgcomp.NewUserMessage("Checkpoint created.", m.styles))
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

	case ShowHelpMsg:
		m.showHelp = true
		m.helpPanel.SetSize(m.width-4, m.height-4)
		m.helpPanel.SetData(m.helpData())

	case msgcomp.RenderInvalidateMsg:
		m.rebuildRenderedContent()

	case input.SendMsg:
		cmds = append(cmds, m.handleSend(msg.Content)...)

	case footerGitRefreshMsg:
		cmds = append(cmds, m.gitRefreshCmd(), m.scheduleGitRefreshCmd())

	case FooterGitMsg:
		m.mu.Lock()
		m.gitBranch = msg.Branch
		m.gitDirty = msg.Dirty
		m.gitAhead = msg.Ahead
		m.gitBehind = msg.Behind
		m.mu.Unlock()
		m.updateFooter()
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
			}
		}
	} else {
		cmds = append(cmds, m.input.UpdateMsg(msg))
		cmds = append(cmds, m.vp.UpdateMsg(msg))
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

	cmds = append(cmds, m.footer.UpdateMsg(msg))
	cmds = append(cmds, m.activity.UpdateMsg(msg))

	m.mu.Lock()
	if m.rb.isDirty() {
		m.vp.SetContent(m.rb.String())
		m.rb.markClean()
	}
	m.mu.Unlock()

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	m.updateFooter()
	m.updateWelcomeData()
	m.updateActivity()

	var mainContent strings.Builder
	if len(m.messages) == 0 {
		mainContent.WriteString(m.welcome.View())
		mainContent.WriteString("\n")
	}
	mainContent.WriteString(m.vp.View().Content)
	mainContent.WriteString("\n")
	if act := m.activity.View(); act != "" {
		mainContent.WriteString(act)
		mainContent.WriteString("\n")
	}
	mainContent.WriteString(m.input.View().Content)
	mainContent.WriteString("\n")
	mainContent.WriteString(m.footer.View())

	view := mainContent.String()

	if m.state == api.TurnWaitingApproval && m.approvalIsActive() {
		if m.approvalFullscreen {
			view = m.renderApprovalFullscreen(view)
		} else {
			view = m.renderApprovalDialog(view)
		}
	}

	if m.sessionPicker != nil {
		view = overlayDialog(view, m.sessionPicker.View(), m.width, m.height)
	}

	if m.showHelp {
		view = overlayDialog(view, m.helpPanel.View().Content, m.width, m.height)
	}

	v := tea.NewView(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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
	m.mu.RLock()
	contextMax := m.contextMax
	if contextMax <= 0 {
		m.mu.RUnlock()
		return
	}
	totalChars := 0
	for _, msg := range m.messages {
		totalChars += len(msg.Content)
	}
	m.mu.RUnlock()
	m.SetContextStats(totalChars/4, contextMax)
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

func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Help overlay takes precedence while it is open.
	if m.showHelp {
		if help.CloseKeys(msg.String()) {
			m.showHelp = false
			return nil
		}
		cmd := m.helpPanel.UpdateMsg(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	// Fullscreen diff preview closes on Esc or Ctrl+E.
	if m.approvalFullscreen {
		if msg.String() == "esc" || msg.String() == "ctrl+e" {
			m.approvalFullscreen = false
			m.approvalDiffContent = ""
		}
		return cmds
	}

	// Approval dialog takes precedence when waiting for approval.
	if m.state == api.TurnWaitingApproval {
		switch msg.String() {
		case "1":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalYes); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "2":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalNo); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "3":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalAlways); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "4", m.config.Keybindings.ApproveDiff:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalDiff); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveYes:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalYes); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveNo:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalNo); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveAlways:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalAlways); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "ctrl+e":
			m.mu.RLock()
			call, ok := m.approval.currentCall()
			session := m.session
			protectedPaths := append([]string(nil), m.protectedPaths...)
			m.mu.RUnlock()
			if !ok || session == nil {
				return cmds
			}
			diff, err := toolCallDiff(call, session.Path, protectedPaths)
			if err == nil && diff != "" {
				m.approvalFullscreen = true
				m.approvalDiffContent = diff
			}
			return cmds
		}
	}

	switch msg.String() {
	case m.config.Keybindings.Quit:
		cmds = append(cmds, tea.Quit)
	case m.config.Keybindings.Cancel:
		// If the user has typed a draft, clear it first instead of cancelling
		// the active stream. A second Cancel then stops the stream.
		if m.input.Value() != "" {
			m.input.SetValue("")
			break
		}
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
			if m.approvalMode == approvalModeAuto {
				m.approvalMode = approvalModeYolo
			} else {
				m.approvalMode = approvalModeAuto
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

func (m *Model) handleMouseMsg(msg tea.MouseReleaseMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}

	l := m.layout()
	welcomeHeight := m.welcomeHeight()
	vpEnd := welcomeHeight + l.vpHeight

	if msg.Y >= vpEnd && msg.Y < l.statusY {
		m.focused = focusInput
	} else if msg.Y >= welcomeHeight && msg.Y < vpEnd {
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
	m.statusText = ""
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
			cancel()
			m.mu.Lock()
			m.streamCancel = nil
			m.streamCh = nil
			m.streamCanceled = true
			m.mu.Unlock()
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

// listSessionsCmd returns a command that lists sessions asynchronously.
func (m *Model) listSessionsCmd() tea.Cmd {
	m.mu.RLock()
	sm := m.sessionManager
	appCtx := m.appCtx
	m.mu.RUnlock()

	timeout := sessionsTimeout
	return func() tea.Msg {
		if sm == nil {
			return SessionsResultMsg{Err: fmt.Errorf("session manager not available")}
		}
		ctx, cancel := context.WithTimeout(appCtx, timeout)
		defer cancel()
		sessions, err := sm.ListAll(ctx, 0)
		return SessionsResultMsg{Sessions: sessions, Err: err}
	}
}

// resumeSessionCmd returns a command that resumes the selected session.
func (m *Model) resumeSessionCmd(id string) tea.Cmd {
	m.mu.RLock()
	sm := m.sessionManager
	appCtx := m.appCtx
	m.mu.RUnlock()

	timeout := sessionsTimeout
	return func() tea.Msg {
		if sm == nil {
			return SessionSelectedMsg{Err: fmt.Errorf("session manager not available")}
		}
		ctx, cancel := context.WithTimeout(appCtx, timeout)
		defer cancel()
		sess, err := sm.Resume(ctx, id)
		return SessionSelectedMsg{Session: sess, Err: err}
	}
}

// checkpointCmd returns a command that creates a checkpoint asynchronously.
func (m *Model) checkpointCmd() tea.Cmd {
	m.mu.RLock()
	gp := m.gitProvider
	appCtx := m.appCtx
	m.mu.RUnlock()

	timeout := checkpointTimeout
	return func() tea.Msg {
		if gp == nil {
			return CheckpointResultMsg{Err: fmt.Errorf("no git provider available")}
		}
		ctx, cancel := context.WithTimeout(appCtx, timeout)
		defer cancel()
		if err := gp.Commit(ctx, ""); err != nil {
			return CheckpointResultMsg{Err: fmt.Errorf("checkpoint failed: %w", err)}
		}
		return CheckpointResultMsg{}
	}
}

// scheduleGitRefreshCmd schedules the next asynchronous git refresh.
func (m *Model) scheduleGitRefreshCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return footerGitRefreshMsg{} })
}

// gitRefreshCmd returns a command that fetches the current git branch and
// dirty state for the footer.
func (m *Model) gitRefreshCmd() tea.Cmd {
	m.mu.RLock()
	gp := m.gitProvider
	appCtx := m.appCtx
	m.mu.RUnlock()

	return func() tea.Msg {
		if gp == nil {
			return FooterGitMsg{}
		}
		ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
		defer cancel()
		repo, err := gp.IsRepo(ctx)
		if err != nil || !repo {
			return FooterGitMsg{}
		}
		branch, _ := gp.Branch(ctx)
		status, _ := gp.Status(ctx)
		dirty := strings.TrimSpace(status) != ""
		return FooterGitMsg{Branch: branch, Dirty: dirty}
	}
}

// readStreamChunk returns a command that reads the next event from the stream.
func (m *Model) readStreamChunk() tea.Cmd {
	ch := m.streamCh // capture at command creation time
	return func() tea.Msg {
		if ch == nil {
			return StreamChunkMsg{Chunk: api.StreamChunk{Done: true}}
		}
		// Guard against stale events after cancellation or a new stream starting.
		m.mu.RLock()
		currentCh := m.streamCh
		canceled := m.streamCanceled
		m.mu.RUnlock()
		if ch != currentCh || canceled {
			return nil
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
		case api.TurnEventApprovalDiff:
			return ApprovalDiffMsg{CallID: event.DiffCallID, Diff: event.DiffContent}
		case api.TurnEventStatus:
			return StatusMsg{Text: event.Content}
		default:
			slog.Warn("unknown turn event type", "type", event.Type)
			return nil
		}
	}
}

func (m *Model) handleStreamChunk(chunk api.StreamChunk) []tea.Cmd {
	var cmds []tea.Cmd

	// Guard against stale buffered chunks after stream cancellation.
	m.mu.RLock()
	streamCh := m.streamCh
	streamCanceled := m.streamCanceled
	m.mu.RUnlock()
	if !chunk.Done && streamCh == nil && streamCanceled {
		return cmds
	}

	if chunk.Error != nil {
		m.addMessage(msgcomp.NewErrorMessage(chunk.Error, m.styles))
		m.setState(api.TurnError)
		m.mu.Lock()
		if m.streamCancel != nil {
			m.streamCancel()
		}
		m.streamCancel = nil
		m.streamCh = nil
		m.streamCanceled = true
		m.rb.setLastBlockStart(0)
		m.mu.Unlock()
		return cmds
	}

	if chunk.Done {
		if m.state != api.TurnError {
			m.setState(api.TurnIdle)
		}
		m.statusText = ""
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
		m.mu.Lock()
		if m.streamCancel != nil {
			m.streamCancel()
		}
		m.streamCancel = nil
		m.streamCh = nil
		m.streamCanceled = false
		m.mu.Unlock()
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
		m.mu.Lock()
		m.messages = append(m.messages, lastMsg)
		m.rb.setLastBlockStart(m.rb.len())
		m.rb.updateLastBlock(lastMsg.View().Content)
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		if m.rb.lastBlockStart() == 0 {
			// After a full rebuild (e.g., resize), recompute the assistant's start position
			m.rb.setLastBlockStart(m.rb.len() - len(lastMsg.View().Content))
			if m.rb.lastBlockStart() < 0 {
				m.rb.setLastBlockStart(0)
			}
		}
		m.mu.Unlock()
	}

	lastMsg.AppendContent(chunk.Content)
	lastMsg.SetWidth(m.vpWidth())

	// Incremental update: truncate back to lastBlockStart and re-render just the last message
	m.mu.Lock()
	m.rb.updateLastBlock(lastMsg.View().Content)
	m.mu.Unlock()

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
	m.statusText = ""
	m.mu.Lock()
	for _, msg := range m.messages {
		if msg.Type == msgcomp.TypeToolCall && msg.ToolCall.ID == result.CallID {
			msg.SetToolResult(result)
			break
		}
	}
	m.mu.Unlock()
	// Re-render the tool call message so the result is visible in the viewport.
	m.rebuildRenderedContent()
	cmds = append(cmds, m.readStreamChunk())
	return cmds
}

func (m *Model) handleApprovalResponse(resp ApprovalResponseMsg) []tea.Cmd {
	var cmds []tea.Cmd

	done, approvals, alwaysAll := m.approvalHandleResponse(resp)

	if resp.Decision == api.ApprovalDiff {
		// Forward the diff request to the turn manager so it can emit a
		// TurnEventApprovalDiff while keeping the pending call active.
		reqID := m.approvalRequestID()
		m.mu.RLock()
		tm := m.turnManager
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := approvalTimeout
		cmds = append(cmds, func() tea.Msg {
			if tm != nil && sessionID != "" {
				ctx, cancel := context.WithTimeout(appCtx, timeout)
				defer cancel()
				if err := tm.ResumeWithApproval(ctx, sessionID, reqID, map[string]api.ApprovalDecision{resp.CallID: api.ApprovalDiff}); err != nil {
					return ErrorMsg{Err: fmt.Errorf("resume with approval: %w", err)}
				}
			}
			return StateChangeMsg{State: api.TurnWaitingApproval}
		})
		cmds = append(cmds, m.readStreamChunk())
		return cmds
	}

	if !done {
		return cmds
	}

	if alwaysAll && m.autoApproveSetter != nil {
		seen := make(map[string]struct{})
		for _, call := range m.approvalPending() {
			if _, ok := seen[call.Name]; ok {
				continue
			}
			seen[call.Name] = struct{}{}
			m.autoApproveSetter(call.Name)
		}
	}

	reqID := m.approvalRequestID()
	m.approvalClear()
	m.setState(api.TurnThinking)

	m.mu.RLock()
	tm := m.turnManager
	var sessionID string
	if m.session != nil {
		sessionID = m.session.ID
	}
	appCtx := m.appCtx
	m.mu.RUnlock()

	timeout := approvalTimeout
	cmds = append(cmds, func() tea.Msg {
		if tm != nil && sessionID != "" {
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			if err := tm.ResumeWithApproval(ctx, sessionID, reqID, approvals); err != nil {
				return ErrorMsg{Err: fmt.Errorf("resume with approval: %w", err)}
			}
		}
		return StateChangeMsg{State: api.TurnThinking}
	})
	cmds = append(cmds, m.readStreamChunk())
	return cmds
}

func (m *Model) addMessage(msg *msgcomp.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg.SetWidth(m.vpWidth())
	m.messages = append(m.messages, msg)
	m.rb.appendBlock(msg.View().Content)
}

// appendSessionMessage adds an existing api.Message to the transcript for
// display. Tool-result messages are skipped because the TUI renders tool calls
// and their results as dedicated tool-call messages.
func (m *Model) appendSessionMessage(msg api.Message) {
	switch msg.Role {
	case api.RoleUser:
		m.addMessage(msgcomp.NewUserMessage(msg.Content, m.styles))
	case api.RoleAssistant:
		if msg.Content != "" {
			m.addMessage(msgcomp.NewAssistantMessage(msg.Content, m.styles))
		}
		for _, tc := range msg.ToolCalls {
			m.addMessage(msgcomp.NewToolCallMessage(tc, m.styles))
		}
	case api.RoleSystem:
		m.addMessage(msgcomp.NewUserMessage("[system]\n"+msg.Content, m.styles))
	}
}

func (m *Model) clearMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]*msgcomp.Message, 0)
	m.rb.reset()
	m.vp.SetContent("")
}

func (m *Model) rebuildRenderedContent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	blocks := make([]string, len(m.messages))
	for i, msg := range m.messages {
		blocks[i] = msg.View().Content
	}
	m.rb.rebuild(blocks)
	m.rebuildCount++
}

func (m *Model) lastAssistantMessage() *msgcomp.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == msgcomp.TypeAssistant {
			return m.messages[i]
		}
	}
	return nil
}

func (m *Model) renderApprovalDialog(background string) string {
	m.mu.RLock()
	call, ok := m.approval.currentCall()
	session := m.session
	protectedPaths := append([]string(nil), m.protectedPaths...)
	m.mu.RUnlock()
	if !ok || session == nil {
		return background
	}
	var b strings.Builder
	b.WriteString("Tool call requires approval\n\n")
	fmt.Fprintf(&b, "Tool: %s\n", call.Name)
	diff, err := toolCallDiff(call, session.Path, protectedPaths)
	switch {
	case err == nil && diff != "":
		fmt.Fprintf(&b, "\n%s\n", diff)
	case err == nil:
		fmt.Fprintf(&b, "Arguments: %s\n", call.Arguments)
	case errors.Is(err, core.ErrDiffFileTooLarge):
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview disabled: file too large)\n", call.Arguments)
	case errors.Is(err, core.ErrDiffPathBlocked):
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview blocked)\n", call.Arguments)
	default:
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview unavailable)\n", call.Arguments)
	}
	fmt.Fprintf(&b, "\n 1. yes  2. no  3. always  4. diff")
	fmt.Fprintf(&b, "\nkeys: 1/2/3/4 | y/n/a/d | ctrl+e fullscreen")

	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

// renderApprovalFullscreen renders a fullscreen diff preview overlay.
func (m *Model) renderApprovalFullscreen(background string) string {
	var b strings.Builder
	b.WriteString("Diff preview (Esc or Ctrl+E to close)\n\n")
	b.WriteString(m.approvalDiffContent)
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

	dialogHeight := lipgloss.Height(dialog)
	dialogWidth := lipgloss.Width(dialog)

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

	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(bgLines, "\n")),
		lipgloss.NewLayer(dialog).X(startX).Y(startY).Z(1),
	)
	rendered := lipgloss.NewCanvas(width, height).Compose(comp).Render()

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

// approvalStartRequest starts a new approval request under m.mu.
func (m *Model) approvalStartRequest(calls []api.ToolCall, requestID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approval.startRequest(calls, requestID)
}

// approvalHandleResponse records one approval decision under m.mu.
func (m *Model) approvalHandleResponse(resp ApprovalResponseMsg) (done bool, approvals map[string]api.ApprovalDecision, alwaysAll bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approval.handleResponse(resp)
}

// approvalRequestID returns the active approval request ID under m.mu.
func (m *Model) approvalRequestID() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.requestID()
}

// approvalPending returns the current pending calls under m.mu.
func (m *Model) approvalPending() []api.ToolCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.pending()
}

// approvalClear resets the approval controller under m.mu.
func (m *Model) approvalClear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approval.clear()
}

// approvalIsActive reports whether there is an active approval request under m.mu.
func (m *Model) approvalIsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.isActive()
}

// approvalApproveCurrent produces a response for the current call under m.mu.
func (m *Model) approvalApproveCurrent(decision api.ApprovalDecision) (ApprovalResponseMsg, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approval.approveCurrent(decision)
}

func (m *Model) updateFooter() {
	m.mu.RLock()
	state := m.state
	modelName := m.modelName
	contextUsed := m.contextUsed
	contextMax := m.contextMax
	toolCount := m.toolCount
	approvalMode := m.approvalMode
	statusText := m.statusText
	cwd := ""
	if m.session != nil {
		cwd = m.session.Path
	}
	gitBranch := m.gitBranch
	gitDirty := m.gitDirty
	gitAhead := m.gitAhead
	gitBehind := m.gitBehind
	m.mu.RUnlock()

	m.footer.SetSize(m.contentWidth())
	m.footer.SetData(footer.Data{
		ModelName:   modelName,
		Mode:        approvalMode,
		State:       state,
		StatusText:  statusText,
		CWD:         cwd,
		ContextUsed: contextUsed,
		ContextMax:  contextMax,
		ToolCount:   toolCount,
		GitBranch:   gitBranch,
		GitDirty:    gitDirty,
		GitAhead:    gitAhead,
		GitBehind:   gitBehind,
	})
}

func (m *Model) updateWelcomeData() {
	// This method is called from layout() and View(), both of which run on the
	// Bubble Tea main goroutine, so direct field access is safe without locking.
	sessionID := ""
	directory := ""
	if m.session != nil {
		sessionID = m.session.ID
		directory = m.session.Path
	}

	m.welcome.SetData(welcome.Data{
		Directory: directory,
		SessionID: sessionID,
		ModelName: m.modelName,
		Version:   welcome.Version,
	})
}

// updateActivity builds the activity panel data from the current turn state and
// pending tool calls. It is safe to call from the Bubble Tea main goroutine.
func (m *Model) updateActivity() {
	m.activity.SetData(activity.Data{
		State:      m.state,
		StatusText: m.statusText,
		ToolCalls:  m.pendingToolCalls(),
	})
}

// pendingToolCalls returns the tool calls that should be shown in the activity
// panel. During TurnToolCalls this is the most recent batch of tool-call
// messages that have not yet received a result.
func (m *Model) pendingToolCalls() []api.ToolCall {
	if m.state != api.TurnToolCalls {
		return nil
	}
	var calls []api.ToolCall
	for _, msg := range m.messages {
		if msg.Type == msgcomp.TypeToolCall && msg.ToolResult == nil {
			calls = append(calls, msg.ToolCall)
		}
	}
	return calls
}

func (m *Model) updateLayout() {
	l := m.layout()
	m.applyLayoutSizes(l)

	// If the layout geometry has not changed, the transcript is still valid.
	if l.eq(m.lastLayout) {
		return
	}

	m.mu.Lock()
	for _, msg := range m.messages {
		msg.SetWidth(l.vpWidth)
	}
	m.mu.Unlock()
	m.rebuildRenderedContent()
	m.lastLayout = l
}

// applyLayoutSizes updates child component sizes without rebuilding the transcript.
func (m *Model) applyLayoutSizes(l layoutRect) {
	m.vp.SetSize(l.contentWidth, l.vpHeight)
	m.input.SetWidth(l.contentWidth)
	m.footer.SetSize(l.contentWidth)
	m.welcome.SetSize(l.contentWidth)
	m.activity.SetSize(l.contentWidth)
}

func (m *Model) refreshFileCandidates() {
	m.mu.RLock()
	provider := m.mentionProvider
	path := ""
	if m.session != nil {
		path = m.session.Path
	}
	m.mu.RUnlock()
	if provider == nil {
		return
	}
	cands, _ := provider.Candidates(path)
	m.input.SetFileCandidates(cands)
}

func (m *Model) contentWidth() int {
	return m.layout().contentWidth
}

func (m *Model) vpWidth() int {
	return m.layout().vpWidth
}
