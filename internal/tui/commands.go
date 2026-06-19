package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/llm"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/welcome"
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

// ShellProgressMsg carries a live output chunk from the quick shell overlay.
type ShellProgressMsg struct {
	Chunk string
}

// ShellResultMsg carries the result of a quick shell overlay command.
type ShellResultMsg struct {
	Command  string
	Output   string
	ExitCode int
	Err      error
}

// shellEvent is an internal progress or result event from a running shell
// command in the quick shell overlay.
type shellEvent struct {
	chunk  string
	result *ShellResultMsg
}

// shellStartedMsg signals that a quick shell command has started and provides
// the channel through which progress and the final result are delivered.
type shellStartedMsg struct {
	Command string
	Ch      <-chan shellEvent
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

// PlanRequestMsg carries a generated plan for user approval.
type PlanRequestMsg struct {
	Plan string
}

// PlanApprovalMsg carries the user's plan approval decision.
type PlanApprovalMsg struct {
	Approved bool
}

// SendMessageMsg is emitted by the root model to the app layer.
type SendMessageMsg struct {
	Content      string
	ContentParts []api.ContentPart
}

// SteerMsg is emitted when the user submits a steering instruction.
type SteerMsg struct {
	Content string
}

// ShowSteerInputMsg opens the steering input overlay.
type ShowSteerInputMsg struct{}

// SteeredMsg is emitted when the turn manager reports a mid-stream steer.
type SteeredMsg struct {
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

// ModelSwitchedMsg carries the new active model and its context window size.
type ModelSwitchedMsg struct {
	Model      string
	ContextMax int
}

// GoalSetMsg carries a user-defined short-term goal.
type GoalSetMsg struct {
	Goal string
}

// BTWMsg carries an out-of-band note to prepend to the next outgoing message.
type BTWMsg struct {
	Note string
}

// ExportResultMsg carries the result of exporting the current session.
type ExportResultMsg struct {
	Path string
	Err  error
}

// ImportResultMsg carries the result of importing a session snapshot.
type ImportResultMsg struct {
	Session *api.Session
	Path    string
	Err     error
}

// debouncedResizeMsg is emitted after the terminal size has settled.
type debouncedResizeMsg struct {
	gen int
}

// FooterGitMsg carries git status information for the footer.
type FooterGitMsg struct {
	Branch string
	Dirty  bool
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
	case "/sessions", "/resume":
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
	case "/export":
		path := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if path == "" {
			name := "session"
			if m.session != nil && m.session.Name != "" {
				name = m.session.Name
			}
			path = name + ".json"
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Exporting session to %q...", path), m.styles))

		m.mu.RLock()
		store := m.store
		var sessionID string
		if m.session != nil {
			sessionID = m.session.ID
		}
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := commandTimeout
		return func() tea.Msg {
			if store == nil || sessionID == "" {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("no session to export")}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			sess, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("get session: %w", err)}
			}
			msgs, err := store.GetMessages(ctx, sessionID, 0)
			if err != nil {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("get messages: %w", err)}
			}
			turns, err := store.GetTurns(ctx, sessionID, 0)
			if err != nil {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("get turns: %w", err)}
			}
			export := api.SessionExport{
				Version:    api.SessionExportVersion,
				ExportedAt: time.Now().UTC(),
				Session:    *sess,
				Messages:   msgs,
				Turns:      turns,
			}
			data, err := json.MarshalIndent(export, "", "  ")
			if err != nil {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("marshal export: %w", err)}
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return ExportResultMsg{Path: path, Err: fmt.Errorf("write file: %w", err)}
			}
			return ExportResultMsg{Path: path}
		}
	case "/import":
		path := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if path == "" {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("usage: /import <path>"), m.styles))
			return nil
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Importing session from %q...", path), m.styles))

		m.mu.RLock()
		store := m.store
		appCtx := m.appCtx
		m.mu.RUnlock()

		timeout := commandTimeout
		return func() tea.Msg {
			if store == nil {
				return ImportResultMsg{Path: path, Err: fmt.Errorf("no store available")}
			}
			data, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return ImportResultMsg{Path: path, Err: fmt.Errorf("read file: %w", err)}
			}
			var export api.SessionExport
			if err := json.Unmarshal(data, &export); err != nil {
				return ImportResultMsg{Path: path, Err: fmt.Errorf("parse export: %w", err)}
			}
			if export.Version != "" && export.Version != api.SessionExportVersion {
				return ImportResultMsg{Path: path, Err: fmt.Errorf("unsupported export version %q", export.Version)}
			}
			ctx, cancel := context.WithTimeout(appCtx, timeout)
			defer cancel()
			created, err := store.CreateSession(ctx, export.Session.Path)
			if err != nil {
				return ImportResultMsg{Path: path, Err: fmt.Errorf("create session: %w", err)}
			}
			cleanup := func() {
				_ = store.DeleteSession(ctx, created.ID)
				_ = store.ClearMessages(ctx, created.ID)
			}
			created.Name = export.Session.Name
			if err := store.UpdateSession(ctx, created); err != nil {
				cleanup()
				return ImportResultMsg{Path: path, Err: fmt.Errorf("update session name: %w", err)}
			}
			for _, msg := range export.Messages {
				if err := store.AppendMessage(ctx, created.ID, msg); err != nil {
					cleanup()
					return ImportResultMsg{Path: path, Err: fmt.Errorf("append message: %w", err)}
				}
			}
			for _, turn := range export.Turns {
				if err := store.SaveTurn(ctx, created.ID, turn); err != nil {
					cleanup()
					return ImportResultMsg{Path: path, Err: fmt.Errorf("save turn: %w", err)}
				}
			}
			return ImportResultMsg{Path: path, Session: created}
		}
	case "/model":
		name := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if name == "" {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("usage: /model <model-name>"), m.styles))
			return nil
		}
		resolved, err := m.resolveModel(name)
		if err != nil {
			m.addMessage(msgcomp.NewErrorMessage(err, m.styles))
			return nil
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Switching model to %q...", resolved), m.styles))
		return func() tea.Msg {
			return ModelSwitchedMsg{Model: resolved, ContextMax: llm.LookupModel(resolved).ContextWindow}
		}
	case "/goal":
		goal := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if goal == "" {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("usage: /goal <text>"), m.styles))
			return nil
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Goal set: %s", goal), m.styles))
		return func() tea.Msg { return GoalSetMsg{Goal: goal} }
	case "/btw":
		note := strings.TrimSpace(strings.TrimPrefix(content, cmd))
		if note == "" {
			m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("usage: /btw <text>"), m.styles))
			return nil
		}
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("Note queued for next message: %s", note), m.styles))
		return func() tea.Msg { return BTWMsg{Note: note} }
	case "/version":
		m.addMessage(msgcomp.NewUserMessage(fmt.Sprintf("kimi-lite version %s", welcome.Version()), m.styles))
		return nil
	case "/help":
		return func() tea.Msg { return ShowHelpMsg{} }
	default:
		m.addMessage(msgcomp.NewErrorMessage(fmt.Errorf("unknown command: %s", cmd), m.styles))
		return nil
	}
}

// resolveModel validates a model name against the configured provider/model
// registry and model aliases, returning the concrete model name to use.
func (m *Model) resolveModel(name string) (string, error) {
	if alias, ok := m.config.Models[name]; ok && alias.Model != "" {
		return alias.Model, nil
	}
	if info := llm.LookupModel(name); info.Provider != "unknown" {
		return name, nil
	}
	for _, p := range m.config.Providers {
		if p.DefaultModel == name {
			return name, nil
		}
	}
	if m.config.LLM.Model == name {
		return name, nil
	}
	return "", fmt.Errorf("unknown model %q", name)
}
