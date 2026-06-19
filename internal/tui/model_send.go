package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"

	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// RunTurnResultMsg carries the result of an asynchronous turn-manager call.
type RunTurnResultMsg struct {
	Ch     <-chan api.TurnEvent
	Err    error
	gen    int
	cancel context.CancelFunc
}

// handleSend processes a user send event. When a turn manager is wired, the
// actual RunTurn* call is dispatched as a tea.Cmd and its result is handled
// asynchronously via RunTurnResultMsg.
func (m *Model) handleSend(content string, parts []api.ContentPart) []tea.Cmd {
	var cmds []tea.Cmd

	if strings.HasPrefix(content, "/") {
		cmd := m.handleCommand(content)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	// Allow sending from Idle or Error state; block only during active turns.
	if m.state == api.TurnThinking || m.state == api.TurnStreaming || m.state == api.TurnToolCalls || m.state == api.TurnWaitingApproval || m.state == api.TurnWaitingPlan {
		return nil
	}

	// Prepend any queued BTW note to the next outgoing message.
	m.mu.Lock()
	if m.btwNote != "" {
		content = m.btwNote + "\n\n" + content
		m.btwNote = ""
	}
	m.mu.Unlock()

	m.toolCount = 0
	m.statusText = ""
	userMsg := msgcomp.NewUserMessage(content, m.styles)
	userMsg.ContentParts = parts
	m.addMessage(userMsg)
	m.setState(api.TurnThinking)

	// Capture service references under lock; the call itself runs asynchronously.
	m.mu.RLock()
	tm := m.turnManager
	session := m.session
	appCtx := m.appCtx
	m.mu.RUnlock()

	if tm != nil && session != nil {
		planMode := m.input.PlanMode()
		m.input.SetPlanMode(false)

		ctx, cancel := context.WithCancel(appCtx)
		m.mu.Lock()
		m.streamGen++
		gen := m.streamGen
		m.streamCancel = cancel
		m.streamCh = nil
		m.streamCanceled = false
		m.mu.Unlock()

		cmds = append(cmds, m.runTurnCmd(ctx, cancel, gen, tm, session.ID, content, parts, planMode))
		return cmds
	}

	// Fallback for tests or when no services are wired.
	cmds = append(cmds, func() tea.Msg {
		return SendMessageMsg{Content: content, ContentParts: parts}
	})
	return cmds
}

// runTurnCmd returns a command that invokes the appropriate turn-manager method
// and wraps the returned channel or error in a RunTurnResultMsg.
func (m *Model) runTurnCmd(ctx context.Context, cancel context.CancelFunc, gen int, tm turnManager, sessionID, content string, parts []api.ContentPart, planMode bool) tea.Cmd {
	return func() tea.Msg {
		var streamCh <-chan api.TurnEvent
		var err error
		if planMode {
			if len(parts) > 0 {
				streamCh, err = tm.RunTurnWithPlanWithContentParts(ctx, sessionID, content, parts)
			} else {
				streamCh, err = tm.RunTurnWithPlan(ctx, sessionID, content)
			}
		} else {
			if len(parts) > 0 {
				streamCh, err = tm.RunTurnWithContentParts(ctx, sessionID, content, parts)
			} else {
				streamCh, err = tm.RunTurn(ctx, sessionID, content)
			}
		}
		return RunTurnResultMsg{Ch: streamCh, Err: err, gen: gen, cancel: cancel}
	}
}

// handleRunTurnResult applies the result of an asynchronous turn-manager call.
func (m *Model) handleRunTurnResult(msg RunTurnResultMsg) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 1)

	m.mu.Lock()
	currentCancel := m.streamCancel
	currentGen := m.streamGen
	m.mu.Unlock()

	// If the stream was cancelled (or replaced by a newer send) while the turn
	// manager call was in flight, discard the stale result.
	if currentCancel == nil || currentGen != msg.gen {
		msg.cancel()
		return cmds
	}

	if msg.Err != nil {
		msg.cancel()
		m.mu.Lock()
		m.streamCancel = nil
		m.streamCh = nil
		m.streamCanceled = true
		m.mu.Unlock()
		m.addMessage(msgcomp.NewErrorMessage(msg.Err, m.styles))
		m.setState(api.TurnError)
		return cmds
	}

	m.mu.Lock()
	m.streamCh = msg.Ch
	m.mu.Unlock()
	cmds = append(cmds, m.readStreamChunk())
	return cmds
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
		case api.TurnEventToolProgress:
			return ToolProgressMsg{CallID: event.CallID, Content: event.Content}
		case api.TurnEventApprovalDiff:
			return ApprovalDiffMsg{CallID: event.DiffCallID, Diff: event.DiffContent}
		case api.TurnEventPlanRequest:
			return PlanRequestMsg{Plan: event.Content}
		case api.TurnEventSteered:
			return SteeredMsg{Content: event.Content}
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

	// Find or create the last assistant message. After a steer event the prior
	// assistant message is finalized and a new one is started.
	lastMsg := m.lastAssistantMessage()
	if lastMsg == nil || m.steeredPending {
		if lastMsg != nil {
			lastMsg.SetStreaming(false)
		}
		m.steeredPending = false
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

func (m *Model) handleSteer(content string) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 1)

	m.mu.RLock()
	tm := m.turnManager
	var sessionID string
	if m.session != nil {
		sessionID = m.session.ID
	}
	appCtx := m.appCtx
	m.mu.RUnlock()

	timeout := commandTimeout
	cmds = append(cmds, func() tea.Msg {
		if tm == nil || sessionID == "" {
			return ErrorMsg{Err: fmt.Errorf("no turn manager")}
		}
		ctx, cancel := context.WithTimeout(appCtx, timeout)
		defer cancel()
		if err := tm.Steer(ctx, sessionID, content); err != nil {
			return ErrorMsg{Err: fmt.Errorf("steer: %w", err)}
		}
		return StateChangeMsg{State: api.TurnThinking}
	})
	return cmds
}
