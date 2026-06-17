package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

var (
	compactTimeout       = 30 * time.Second
	clearMessagesTimeout = 10 * time.Second
	commandTimeout       = 30 * time.Second
	sessionsTimeout      = 10 * time.Second
	checkpointTimeout    = 10 * time.Second
	approvalTimeout      = 60 * time.Second
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

// ToolProgressMsg carries a live output chunk from a running tool call.
type ToolProgressMsg struct {
	CallID  string
	Content string
}

// StatusMsg carries a transient status sentence for the status bar.
type StatusMsg struct {
	Text string
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

// ApprovalDiffMsg carries a diff preview for a pending tool call.
type ApprovalDiffMsg struct {
	CallID string
	Diff   string
}

// SendMessageMsg is emitted by the root model to the app layer.
type SendMessageMsg struct {
	Content string
}

// CompactMsg signals the app to compact the session.
type CompactMsg struct{}

// CompactResultMsg carries the result of a compaction operation.
type CompactResultMsg struct {
	Count   int
	Summary string
}

// ClearMsg signals the app to clear messages.
type ClearMsg struct{}

// SessionsMsg signals the app to list sessions.
type SessionsMsg struct{}

// SessionsResultMsg carries the result of listing sessions.
type SessionsResultMsg struct {
	Sessions []api.Session
	Err      error
}

// SessionSelectedMsg carries the result of resuming a session from the picker.
type SessionSelectedMsg struct {
	Session *api.Session
	Err     error
}

// CheckpointMsg signals the app to create a checkpoint.
type CheckpointMsg struct{}

// CheckpointResultMsg carries the result of a checkpoint operation.
type CheckpointResultMsg struct {
	Err error
}

// MCPListMsg carries the result of listing MCP tools.
type MCPListMsg struct {
	Tools []api.ToolDefinition
}

// SetTitleMsg carries the new name for the current session.
type SetTitleMsg struct {
	Name string
}

// ForkResultMsg carries the result of forking the current session.
type ForkResultMsg struct {
	Session *api.Session
}

// debouncedResizeMsg is emitted after the terminal size has settled.
type debouncedResizeMsg struct {
	gen int
}

// FooterGitMsg carries git status information for the footer.
type FooterGitMsg struct {
	Branch string
	Dirty  bool
	Ahead  int
	Behind int
}

// ShowHelpMsg opens the help overlay.
type ShowHelpMsg struct{}

// footerGitRefreshMsg triggers an asynchronous git status refresh.
type footerGitRefreshMsg struct{}

func (m *Model) handleCommand(content string) tea.Cmd {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]

	switch cmd {
	case "/compact":
		m.addMessage(msgcomp.NewUserMessage("Compacting session...", m.styles))

		// Capture the fields needed by the async command while holding the lock.
		m.mu.RLock()
		comp := m.compressor
		store := m.store
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		keepRecent := m.config.Behavior.CompactKeepRecent
		contextMax := m.contextMax
		appCtx := m.appCtx
		m.mu.RUnlock()

		if keepRecent <= 0 {
			keepRecent = 2
		}
		if contextMax > 0 {
			keepRecent = max(keepRecent, min(8, contextMax/64000))
		}

		timeout := compactTimeout
		return func() tea.Msg {
			if comp != nil && store != nil && sessionID != "" {
				ctx, cancel := context.WithTimeout(appCtx, timeout)
				defer cancel()
				summarized, err := comp.Compact(ctx, store, sessionID, keepRecent)
				if err != nil {
					return ErrorMsg{Err: err}
				}
				summary := ""
				if summarized > 0 {
					if msgs, getErr := store.GetMessages(ctx, sessionID, 0); getErr == nil {
						for _, msg := range msgs {
							if msg.Role == api.RoleSystem && strings.HasPrefix(msg.Content, "Previous conversation summary:") {
								summary = strings.TrimSpace(strings.TrimPrefix(msg.Content, "Previous conversation summary:"))
								break
							}
						}
					}
				}
				return CompactResultMsg{Count: summarized, Summary: summary}
			}
			return CompactMsg{}
		}
	case "/clear":
		if m.state == api.TurnStreaming || m.state == api.TurnThinking {
			return nil
		}
		m.clearMessages()

		m.mu.RLock()
		sm := m.sessionManager
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := clearMessagesTimeout
		return func() tea.Msg {
			if sm != nil && sessionID != "" {
				ctx, cancel := context.WithTimeout(appCtx, timeout)
				defer cancel()
				if err := sm.ClearMessages(ctx, sessionID); err != nil {
					return ErrorMsg{Err: fmt.Errorf("clear messages: %w", err)}
				}
			}
			return ClearMsg{}
		}
	case "/sessions":
		m.addMessage(msgcomp.NewUserMessage("Listing sessions...", m.styles))
		return m.listSessionsCmd()

	case "/checkpoint":
		m.addMessage(msgcomp.NewUserMessage("Creating checkpoint...", m.styles))
		return m.checkpointCmd()
	case "/diff":
		args := strings.TrimSpace(strings.TrimPrefix(content, cmd))

		m.mu.RLock()
		gp := m.gitProvider
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := commandTimeout
		return func() tea.Msg {
			if gp == nil {
				return ErrorMsg{Err: fmt.Errorf("no git provider available")}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			diff, err := gp.Diff(ctx, args)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("git diff: %w", err)}
			}
			if diff == "" {
				return ErrorMsg{Err: fmt.Errorf("no diff for %s", args)}
			}
			return StreamChunkMsg{Chunk: api.StreamChunk{Content: diff}}
		}
	case "/mcp":
		m.mu.RLock()
		mc := m.mcpClient
		appCtx := m.appCtx
		m.mu.RUnlock()

		if mc != nil {
			m.addMessage(msgcomp.NewUserMessage("Listing MCP tools...", m.styles))
		}
		timeout := commandTimeout
		return func() tea.Msg {
			if mc == nil {
				return MCPListMsg{Tools: nil}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			tools, err := mc.ListTools(ctx)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("list mcp tools: %w", err)}
			}
			return MCPListMsg{Tools: tools}
		}
	case "/title":
		name := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if name == "" {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("usage: /title <name>"), m.styles))
			return nil
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Renaming session to %q...", name), m.styles))

		m.mu.RLock()
		sm := m.sessionManager
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := commandTimeout
		return func() tea.Msg {
			if sm == nil || sessionID == "" {
				return ErrorMsg{Err: fmt.Errorf("no session to rename")}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			if err := sm.Rename(ctx, sessionID, name); err != nil {
				return ErrorMsg{Err: fmt.Errorf("rename session: %w", err)}
			}
			return SetTitleMsg{Name: name}
		}
	case "/fork":
		name := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		m.addMessage(msgcomp.NewUserMessage("Forking session...", m.styles))

		m.mu.RLock()
		sm := m.sessionManager
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := commandTimeout
		return func() tea.Msg {
			if sm == nil || sessionID == "" {
				return ErrorMsg{Err: fmt.Errorf("no session to fork")}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			sess, err := sm.Fork(ctx, sessionID, name)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("fork session: %w", err)}
			}
			return ForkResultMsg{Session: sess}
		}
	case "/help":
		return func() tea.Msg { return ShowHelpMsg{} }
	default:
		m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("unknown command: %s", cmd), m.styles))
		return nil
	}
}
