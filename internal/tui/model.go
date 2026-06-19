// Package tui provides the Terminal UI layer for kimi-lite using Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/activity"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/clipboard"
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

	// approvalModeManual, approvalModeAuto and approvalModeYolo mirror core
	// approval modes in the integer representation used by the TUI's
	// approval-mode callback.
	approvalModeManual = int(core.ModeManual)
	approvalModeAuto   = int(core.ModeAuto)
	approvalModeYolo   = int(core.ModeYolo)

	// maxToolProgressLen caps live tool output stored in the root model.
	maxToolProgressLen = 2048
)

// inputHeight returns the current rendered height of the input component.
func (m *Model) inputHeight() int {
	h := m.input.Height()
	if h < minInputHeight {
		return minInputHeight
	}
	return h
}

// turnManager is the interface needed from core.TurnManager.
type turnManager interface {
	RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error)
	RunTurnWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error)
	RunTurnWithPlan(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error)
	RunTurnWithPlanWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error)
	ResumeWithPlan(ctx context.Context, sessionID string, approved bool) error
	ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error
	Steer(ctx context.Context, sessionID string, input string) error
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
	// approvalDiffCallID identifies the call whose diff is cached in
	// approvalDiffContent. An empty value means no diff has been cached yet.
	approvalDiffCallID string
	// approvalDiffErr holds the error from computing the cached diff, if any.
	approvalDiffErr error
	// approvalFullscreenPendingReqID is the request ID waiting for an async
	// diff before opening fullscreen. Zero means no fullscreen is pending.
	approvalFullscreenPendingReqID int64

	// planRequest holds the generated plan waiting for user approval.
	planRequest string
	// planPending is true when the plan approval panel is open.
	planPending bool
	// planScrollOffset is the first visible line of the wrapped plan text.
	planScrollOffset int

	// steerOpen is true when the steering input overlay is visible.
	steerOpen bool
	// steerInput holds the draft steering instruction.
	steerInput string
	// steerCursor is the cursor position in runes within steerInput.
	steerCursor int
	// steeredPending is true when a steer event was received and the next
	// streamed assistant message must start a new block.
	steeredPending bool

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
	streamGen      int  // increments for each new send to identify stale results

	// Live output from running tools, keyed by call ID.
	toolProgress map[string]string

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

// New creates the root TUI model. themeConfigDir is the directory that
// contains user-defined themes under a "themes" subdirectory.
func New(cfg *api.Config, session *api.Session, appCtx context.Context, themeConfigDir string) (*Model, error) {
	if _, err := os.Stat(session.Path); err != nil {
		return nil, fmt.Errorf("session path %q: %w", session.Path, err)
	}

	theme, err := styles.LoadTheme(cfg.UI.Theme, themeConfigDir)
	if err != nil {
		return nil, fmt.Errorf("load theme: %w", err)
	}
	st := styles.NewFromTheme(theme)

	inp := input.New(st, input.ConfigurableKeyMap(cfg.Keybindings), cfg.Session.MaxHistory)
	inp.SetEditor(cfg.UI.Editor)
	inp.SetContext(appCtx)
	inp.SetConfigDir(themeConfigDir)
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
		toolProgress:    make(map[string]string),
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

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// effectiveKeybindings returns the configured keybindings with empty values
// replaced by their defaults so handlers and help stay consistent.
type effectiveKeybindings struct {
	Send           string
	Newline        string
	Cancel         string
	Quit           string
	Yolo           string
	FocusNext      string
	FocusPrev      string
	ApproveYes     string
	ApproveNo      string
	ApproveAlways  string
	ApproveDiff    string
	ExternalEditor string
	Steer          string
	Paste          string
}

func (m *Model) effectiveKeybindings() effectiveKeybindings {
	kb := m.config.Keybindings
	return effectiveKeybindings{
		Send:           defaultIfEmpty(kb.Send, "enter"),
		Newline:        defaultIfEmpty(kb.Newline, "alt+enter"),
		Cancel:         defaultIfEmpty(kb.Cancel, "esc"),
		Quit:           defaultIfEmpty(kb.Quit, "ctrl+c"),
		Yolo:           defaultIfEmpty(kb.Yolo, "ctrl+y"),
		FocusNext:      defaultIfEmpty(kb.FocusNext, "tab"),
		FocusPrev:      defaultIfEmpty(kb.FocusPrev, "shift+tab"),
		ApproveYes:     defaultIfEmpty(kb.ApproveYes, "y"),
		ApproveNo:      defaultIfEmpty(kb.ApproveNo, "n"),
		ApproveAlways:  defaultIfEmpty(kb.ApproveAlways, "a"),
		ApproveDiff:    defaultIfEmpty(kb.ApproveDiff, "d"),
		ExternalEditor: defaultIfEmpty(kb.ExternalEditor, "ctrl+g"),
		Steer:          defaultIfEmpty(kb.Steer, "ctrl+s"),
		Paste:          defaultIfEmpty(kb.Paste, "ctrl+v"),
	}
}

func (m *Model) helpData() help.Data {
	kb := m.effectiveKeybindings()

	shortcuts := []help.Shortcut{
		{Keys: kb.Send + " (input)", Description: "Send message"},
		{Keys: kb.Newline, Description: "Insert newline"},
		{Keys: kb.FocusNext, Description: "Switch focus"},
		{Keys: kb.FocusPrev, Description: "Toggle plan mode"},
		{Keys: kb.ExternalEditor, Description: "External editor"},
		{Keys: kb.Yolo, Description: "Toggle yolo mode"},
		{Keys: kb.Steer, Description: "Steer response while streaming"},
		{Keys: kb.Paste, Description: "Paste image or file"},
		{Keys: "r", Description: "Toggle raw markdown (viewport focus)"},
		{Keys: kb.Send + " (viewport, tool call)", Description: "Expand/collapse tool call"},
		{Keys: kb.Cancel, Description: "Cancel / clear draft"},
		{Keys: kb.Quit, Description: "Quit"},
		{Keys: "?", Description: "Toggle this help"},
	}

	if m.state == api.TurnWaitingApproval {
		shortcuts = append(shortcuts, help.Shortcut{
			Keys:        kb.ApproveYes + "/" + kb.ApproveNo + "/" + kb.ApproveAlways + "/" + kb.ApproveDiff,
			Description: "Approve yes/no/always/diff",
		})
	}
	if m.planPending {
		shortcuts = append(shortcuts,
			help.Shortcut{Keys: kb.Send + "/y", Description: "Approve plan"},
			help.Shortcut{Keys: kb.Cancel + "/n", Description: "Reject plan"},
		)
	}
	if m.steerOpen {
		shortcuts = append(shortcuts, help.Shortcut{
			Keys:        kb.Send,
			Description: "Send steering instruction",
		})
	}

	return help.Data{
		Shortcuts: shortcuts,
		Commands: []help.SlashCommand{
			{Name: "/compact", Description: "Summarize older messages"},
			{Name: "/clear", Description: "Clear transcript"},
			{Name: "/sessions, /resume", Description: "Switch session"},
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
		m.input.RefreshCandidatesCmd(),
		m.scheduleGitRefreshCmd(),
	)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.sessionPicker != nil {
			done, selected, copyCmd := m.sessionPicker.Update(msg)
			if selected {
				id := m.sessionPicker.Selected().ID
				m.sessionPicker = nil
				m.statusText = ""
				if id != "" {
					cmds = append(cmds, m.resumeSessionCmd(id))
				}
				return m, tea.Batch(cmds...)
			}
			if done {
				m.sessionPicker = nil
				m.statusText = ""
				return m, nil
			}
			var currentPath string
			m.mu.RLock()
			if m.session != nil {
				currentPath = m.session.Path
			}
			m.mu.RUnlock()
			if sel := m.sessionPicker.Selected(); sel.Path != "" && currentPath != "" && sel.Path != currentPath {
				m.statusText = fmt.Sprintf("cd %s && kimi-lite --session %s", shellQuote(sel.Path), sel.ID)
			} else {
				m.statusText = ""
			}
			if copyCmd && m.statusText != "" {
				cmds = append(cmds, m.copyStatusTextCmd())
			}
			return m, tea.Batch(cmds...)
		}
		// Let the input component consume completion keys while a popup is open.
		if !m.input.Completing() {
			cmds = append(cmds, m.handleKeyMsg(msg)...)
		}
		// Overlays own the keyboard while open; do not dispatch the key to
		// child components.
		if m.planPending || m.showHelp || m.approvalFullscreen || m.steerOpen {
			return m, tea.Batch(cmds...)
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

	case ToolProgressMsg:
		m.appendToolProgress(msg.CallID, msg.Content)
		m.updateActivity()

	case ToolResultMsg:
		m.mu.Lock()
		delete(m.toolProgress, msg.Result.CallID)
		m.mu.Unlock()
		m.updateActivity()
		cmds = append(cmds, m.handleToolResult(msg.Result)...)

	case ErrorMsg:
		m.addMessage(msgcomp.NewErrorMessage(msg.Err, m.styles))
		m.setState(api.TurnError)
		m.statusText = ""

	case StatusMsg:
		m.statusText = msg.Text

	case input.PasteErrorMsg:
		m.statusText = msg.Err.Error()

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
		cmds = append(cmds, m.readStreamChunk(), m.approvalComputeDiffCmd())

	case ApprovalResponseMsg:
		cmds = append(cmds, m.handleApprovalResponse(msg)...)

	case ApprovalDiffMsg:
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Diff preview for %s:\n%s", msg.CallID, msg.Diff), m.styles))
		m.setState(api.TurnWaitingApproval)

	case approvalDiffComputedMsg:
		m.mu.Lock()
		if msg.RequestID == m.approval.requestID() {
			callMatches := false
			if call, ok := m.approval.currentCall(); ok && call.ID == msg.CallID {
				m.approvalDiffCallID = msg.CallID
				m.approvalDiffContent = msg.Diff
				m.approvalDiffErr = msg.Err
				callMatches = true
			}
			if m.approvalFullscreenPendingReqID == msg.RequestID {
				m.approvalFullscreenPendingReqID = 0
				if callMatches && msg.Err == nil && msg.Diff != "" {
					m.approvalFullscreen = true
				}
			}
		}
		m.mu.Unlock()

	case PlanRequestMsg:
		m.planRequest = msg.Plan
		m.planPending = true
		m.planScrollOffset = 0
		m.setState(api.TurnWaitingPlan)

	case ShowSteerInputMsg:
		m.steerOpen = true

	case SteerMsg:
		cmds = append(cmds, m.handleSteer(msg.Content)...)

	case SteeredMsg:
		if lastMsg := m.lastAssistantMessage(); lastMsg != nil {
			lastMsg.SetStreaming(false)
		}
		m.addMessage(msgcomp.NewUserMessage(msg.Content, m.styles))
		m.steeredPending = true
		m.rebuildRenderedContent()

	case PlanApprovalMsg:
		m.planPending = false
		m.planRequest = ""
		m.mu.RLock()
		tm := m.turnManager
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()
		cmds = append(cmds, func() tea.Msg {
			if tm == nil || sessionID == "" {
				return ErrorMsg{Err: fmt.Errorf("no turn manager")}
			}
			ctx, cancel := context.WithTimeout(appCtx, commandTimeout)
			defer cancel()
			if err := tm.ResumeWithPlan(ctx, sessionID, msg.Approved); err != nil {
				return ErrorMsg{Err: fmt.Errorf("resume plan: %w", err)}
			}
			return StateChangeMsg{State: api.TurnThinking}
		})

	case SessionsMsg:
		cmds = append(cmds, m.listSessionsCmd())

	case SessionsResultMsg:
		if msg.Err != nil {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("list sessions: %w", msg.Err), m.styles))
		} else if len(msg.Sessions) == 0 {
			m.addMessage(msgcomp.NewUserMessage("No sessions found.", m.styles))
		} else {
			m.mu.RLock()
			var path string
			if m.session != nil {
				path = m.session.Path
			}
			m.mu.RUnlock()
			m.sessionPicker = sessions.NewPicker(msg.Sessions, path, m.width, m.height, m.styles)
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
			cmds = append(cmds, m.input.RefreshCandidatesCmd())
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
		cmds = append(cmds, m.handleSend(msg.Content, msg.ContentParts)...)

	case RunTurnResultMsg:
		cmds = append(cmds, m.handleRunTurnResult(msg)...)

	case footerGitRefreshMsg:
		cmds = append(cmds, m.gitRefreshCmd(), m.scheduleGitRefreshCmd())

	case FooterGitMsg:
		m.mu.Lock()
		m.gitBranch = msg.Branch
		m.gitDirty = msg.Dirty
		m.mu.Unlock()
		m.updateFooter()
	}

	// Update focused child component for KeyPressMsg; all children for other messages
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		keyStr := keyMsg.String()
		isTab := keyStr == m.config.Keybindings.FocusNext || keyStr == m.config.Keybindings.FocusPrev
		// Tab/Shift+Tab are normally focus-cycle keys, but when a completion popup
		// is open they must reach the input for completion navigation.
		if !isTab || m.input.Completing() {
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

	if m.planPending {
		view = m.renderPlanPanel(view)
	}

	if m.steerOpen {
		view = m.renderSteerOverlay(view)
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

// copyStatusTextCmd returns a command that copies the current status text to
// the system clipboard.
func (m *Model) copyStatusTextCmd() tea.Cmd {
	m.mu.RLock()
	appCtx := m.appCtx
	text := m.statusText
	m.mu.RUnlock()

	return func() tea.Msg {
		if text == "" {
			return nil
		}
		if err := clipboard.WriteText(appCtx, text); err != nil {
			return ErrorMsg{Err: fmt.Errorf("copy to clipboard: %w", err)}
		}
		return StatusMsg{Text: "Copied resume command to clipboard"}
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
		cmds = append(cmds, m.approvalComputeDiffCmd())
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
	m.approvalFullscreen = false
	m.approvalDiffContent = ""
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
		userMsg := msgcomp.NewUserMessage(msg.Content, m.styles)
		userMsg.ContentParts = msg.ContentParts
		m.addMessage(userMsg)
	case api.RoleAssistant:
		if msg.Content != "" || len(msg.ContentParts) > 0 {
			assistantMsg := msgcomp.NewAssistantMessage(msg.Content, m.styles)
			assistantMsg.ContentParts = msg.ContentParts
			m.addMessage(assistantMsg)
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
	m.mu.RUnlock()

	m.footer.SetSize(m.contentWidth())
	m.footer.SetData(footer.Data{
		ModelName:   modelName,
		Mode:        approvalMode,
		PlanMode:    m.input.PlanMode(),
		State:       state,
		StatusText:  statusText,
		CWD:         cwd,
		ContextUsed: contextUsed,
		ContextMax:  contextMax,
		ToolCount:   toolCount,
		GitBranch:   gitBranch,
		GitDirty:    gitDirty,
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
		Version:   welcome.Version(),
	})
}

// updateActivity builds the activity panel data from the current turn state and
// pending tool calls. It is safe to call from the Bubble Tea main goroutine.
func (m *Model) updateActivity() {
	m.mu.RLock()
	state := m.state
	statusText := m.statusText
	toolProgress := maps.Clone(m.toolProgress)
	m.mu.RUnlock()
	m.activity.SetData(activity.Data{
		State:       state,
		StatusText:  statusText,
		ToolCalls:   m.pendingToolCalls(),
		ToolOutputs: toolProgress,
	})
}

// appendToolProgress appends a chunk to the live output for the given call ID,
// capping the stored output to maxToolProgressLen bytes.
func (m *Model) appendToolProgress(callID, chunk string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolProgress[callID] += chunk
	if len(m.toolProgress[callID]) > maxToolProgressLen {
		m.toolProgress[callID] = "…" + m.toolProgress[callID][len(m.toolProgress[callID])-maxToolProgressLen+1:]
	}
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

func (m *Model) vpWidth() int {
	contentWidth := m.width
	if contentWidth < minContentWidth {
		contentWidth = minContentWidth
	}
	return contentWidth - viewportWidthPadding
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
