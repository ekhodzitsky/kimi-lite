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

// CompactResultMsg carries the result of a compaction operation.
type CompactResultMsg struct {
	Count int
}

// ClearMsg signals the app to clear messages.
type ClearMsg struct{}

// SessionsMsg signals the app to list sessions.
type SessionsMsg struct{}

// CheckpointMsg signals the app to create a checkpoint.
type CheckpointMsg struct{}

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

func (m *Model) handleCommand(content string) tea.Cmd {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]

	switch cmd {
	case "/compact":
		m.addMessage(msgcomp.NewUserMessage("Compacting session...", m.styles))
		return func() tea.Msg {
			if m.compressor != nil && m.store != nil && m.session != nil {
				keepRecent := m.config.Behavior.CompactKeepRecent
				if keepRecent <= 0 {
					keepRecent = 2
				}
				if m.contextMax > 0 {
					keepRecent = max(keepRecent, min(8, m.contextMax/64000))
				}
				ctx, cancel := context.WithTimeout(m.appCtx, compactTimeout)
				defer cancel()
				summarized, err := m.compressor.Compact(ctx, m.store, m.session.ID, keepRecent)
				if err != nil {
					return ErrorMsg{Err: err}
				}
				return CompactResultMsg{Count: summarized}
			}
			return CompactMsg{}
		}
	case "/clear":
		if m.state == api.TurnStreaming || m.state == api.TurnThinking {
			return nil
		}
		m.clearMessages()
		return func() tea.Msg {
			if m.sessionManager != nil && m.session != nil {
				ctx, cancel := context.WithTimeout(m.appCtx, clearMessagesTimeout)
				defer cancel()
				_ = m.sessionManager.ClearMessages(ctx, m.session.ID)
			}
			return ClearMsg{}
		}
	case "/sessions":
		m.addMessage(msgcomp.NewUserMessage("Listing sessions...", m.styles))
		return func() tea.Msg { return SessionsMsg{} }

	case "/checkpoint":
		m.addMessage(msgcomp.NewUserMessage("Creating checkpoint...", m.styles))
		return func() tea.Msg { return CheckpointMsg{} }
	case "/diff":
		args := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		return func() tea.Msg {
			if m.gitProvider == nil {
				return ErrorMsg{Err: fmt.Errorf("no git provider available")}
			}
			diff, err := m.gitProvider.Diff(context.Background(), args)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("git diff: %w", err)}
			}
			if diff == "" {
				return ErrorMsg{Err: fmt.Errorf("no diff for %s", args)}
			}
			return StreamChunkMsg{Chunk: api.StreamChunk{Content: diff}}
		}
	case "/mcp":
		if m.mcpClient == nil {
			m.addMessage(msgcomp.NewUserMessage("No MCP tools connected.", m.styles))
		} else {
			m.addMessage(msgcomp.NewUserMessage("Listing MCP tools...", m.styles))
		}
		return func() tea.Msg {
			if m.mcpClient == nil {
				return MCPListMsg{Tools: nil}
			}
			tools, err := m.mcpClient.ListTools(context.Background())
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
		return func() tea.Msg {
			if m.sessionManager == nil || m.session == nil {
				return ErrorMsg{Err: fmt.Errorf("no session to rename")}
			}
			if err := m.sessionManager.Rename(context.Background(), m.session.ID, name); err != nil {
				return ErrorMsg{Err: fmt.Errorf("rename session: %w", err)}
			}
			return SetTitleMsg{Name: name}
		}
	case "/fork":
		name := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		m.addMessage(msgcomp.NewUserMessage("Forking session...", m.styles))
		return func() tea.Msg {
			if m.sessionManager == nil || m.session == nil {
				return ErrorMsg{Err: fmt.Errorf("no session to fork")}
			}
			sess, err := m.sessionManager.Fork(context.Background(), m.session.ID, name)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("fork session: %w", err)}
			}
			return ForkResultMsg{Session: sess}
		}
	default:
		m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("unknown command: %s", cmd), m.styles))
		return nil
	}
}
