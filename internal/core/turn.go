package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const maxStreamResponseSize = 10 * 1024 * 1024 // 10 MB

// TurnManager orchestrates a single user input → LLM response cycle.
type TurnManager struct {
	llm      api.LLMClient
	tools    api.ToolExecutor
	approval api.ApprovalGate
	store    api.Store
	cfg      api.ConfigProvider
	mu       sync.RWMutex
	turn     *api.Turn

	pendingMu    sync.Mutex
	pendingCalls []api.ToolCall
	approvalCh   chan map[string]api.ApprovalDecision
	wg           sync.WaitGroup
	running      atomic.Bool
}

// NewTurnManager creates a new TurnManager.
func NewTurnManager(llm api.LLMClient, tools api.ToolExecutor, approval api.ApprovalGate, store api.Store, cfg api.ConfigProvider) *TurnManager {
	return &TurnManager{
		llm:        llm,
		tools:      tools,
		approval:   approval,
		store:      store,
		cfg:        cfg,
		approvalCh: make(chan map[string]api.ApprovalDecision, 1),
	}
}

// CurrentTurn returns a copy of the current turn.
func (tm *TurnManager) CurrentTurn() *api.Turn {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.turn == nil {
		return nil
	}
	t := *tm.turn
	return &t
}

// PendingApprovals returns a copy of the currently pending tool calls awaiting approval.
func (tm *TurnManager) PendingApprovals() []api.ToolCall {
	tm.pendingMu.Lock()
	defer tm.pendingMu.Unlock()
	return append([]api.ToolCall(nil), tm.pendingCalls...)
}

// ResumeWithApproval resumes a turn that is waiting for manual approval.
func (tm *TurnManager) ResumeWithApproval(ctx context.Context, sessionID string, approvals map[string]api.ApprovalDecision) error {
	tm.pendingMu.Lock()
	defer tm.pendingMu.Unlock()

	if len(tm.pendingCalls) == 0 {
		return fmt.Errorf("no pending approvals")
	}

	tm.mu.RLock()
	turn := tm.turn
	tm.mu.RUnlock()
	if turn == nil {
		return fmt.Errorf("no active turn")
	}

	select {
	case tm.approvalCh <- approvals:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait blocks until all in-flight turns complete.
func (tm *TurnManager) Wait() {
	tm.wg.Wait()
}

// RunTurn executes a complete turn for the given session and user input.
// It returns a channel that streams response content chunks.
// Returns an error if a turn is already in progress.
func (tm *TurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan string, error) {
	if !tm.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("turn already in progress")
	}
	outCh, err := tm.startTurn(ctx, sessionID, input)
	if err != nil {
		tm.running.Store(false)
		return nil, err
	}
	return outCh, nil
}

func (tm *TurnManager) startTurn(ctx context.Context, sessionID string, input string) (<-chan string, error) {
	if tm.cfg != nil && tm.cfg.Get() != nil {
		maxTurns := tm.cfg.Get().Behavior.MaxTurns
		if maxTurns > 0 {
			turns, err := tm.store.GetTurns(ctx, sessionID, 100)
			if err != nil {
				return nil, fmt.Errorf("get turns: %w", err)
			}
			if len(turns) >= maxTurns {
				return nil, fmt.Errorf("max turns (%d) reached for session %s", maxTurns, sessionID)
			}
		}
	}

	// Drain any stale approval from previous turn to prevent cross-turn contamination.
	select {
	case <-tm.approvalCh:
	default:
	}
	tm.pendingMu.Lock()
	tm.pendingCalls = nil
	tm.pendingMu.Unlock()

	turn := &api.Turn{
		ID:        idgen.GenerateID(),
		State:     api.TurnThinking,
		Input:     input,
		StartedAt: time.Now().UTC(),
	}

	tm.mu.Lock()
	tm.turn = turn
	tm.mu.Unlock()

	if err := tm.saveTurn(ctx, sessionID, turn); err != nil {
		return nil, fmt.Errorf("save turn: %w", err)
	}

	userMsg := api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleUser,
		Content:   input,
		CreatedAt: time.Now().UTC(),
	}
	if err := tm.store.AppendMessage(ctx, sessionID, userMsg); err != nil {
		return nil, fmt.Errorf("append user message: %w", err)
	}

	msgLimit := 1000
	if tm.cfg != nil && tm.cfg.Get() != nil && tm.cfg.Get().Session.MaxHistory > 0 {
		msgLimit = tm.cfg.Get().Session.MaxHistory
	}
	messages, err := tm.store.GetMessages(ctx, sessionID, msgLimit)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	tools := tm.tools.Definitions()

	tm.mu.Lock()
	turn.State = api.TurnStreaming
	tm.mu.Unlock()
	if err := tm.saveTurn(ctx, sessionID, turn); err != nil {
		slog.Error("failed to save turn", "error", err)
	}

	// Start the first LLM stream synchronously so that immediate errors are returned.
	streamCh, err := tm.llm.ChatStream(ctx, messages, tools)
	if err != nil {
		tm.setError(ctx, sessionID, turn, fmt.Errorf("chat stream: %w", err))
		return nil, fmt.Errorf("chat stream: %w", err)
	}

	outCh := make(chan string, 16)
	tm.wg.Add(1)
	go tm.run(ctx, sessionID, turn, messages, tools, streamCh, outCh, msgLimit)

	return outCh, nil
}

func (tm *TurnManager) run(ctx context.Context, sessionID string, turn *api.Turn, messages []api.Message, tools []api.ToolDefinition, firstStream <-chan api.StreamChunk, outCh chan string, msgLimit int) {
	defer tm.running.Store(false)
	defer tm.wg.Done()
	defer close(outCh)

	content, toolCalls, err := tm.consumeStream(ctx, sessionID, turn, firstStream, outCh)
	if err != nil {
		return
	}

	tm.mu.Lock()
	turn.Response = content
	turn.ToolCalls = toolCalls
	if len(toolCalls) > 0 {
		turn.State = api.TurnToolCalls
	}
	tm.mu.Unlock()
	if err := tm.saveTurn(ctx, sessionID, turn); err != nil {
		slog.Error("failed to save turn", "error", err)
	}

	const maxToolRounds = 10
	for round := 0; len(toolCalls) > 0; round++ {
		if round >= maxToolRounds {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("max tool rounds (%d) exceeded", maxToolRounds))
			return
		}
		results, pending := tm.executeToolCalls(ctx, sessionID, turn, toolCalls)

		if len(pending) > 0 {
			var approvals map[string]api.ApprovalDecision
			select {
			case approvals = <-tm.approvalCh:
			case <-ctx.Done():
				tm.setError(ctx, sessionID, turn, ctx.Err())
				return
			}

			for _, call := range pending {
				decision, ok := approvals[call.ID]
				if !ok {
					decision = api.ApprovalNo
				}
				if decision == api.ApprovalDiff {
					decision = api.ApprovalNo
				}
				var result api.ToolResult
				if decision == api.ApprovalNo {
					result = api.ToolResult{
						CallID: call.ID,
						Name:   call.Name,
						Error:  "tool call denied",
					}
				} else {
					result, err = tm.tools.Execute(ctx, call)
					if err != nil {
						result.Error = err.Error()
					}
				}
				results = append(results, result)
			}

			tm.pendingMu.Lock()
			tm.pendingCalls = nil
			tm.pendingMu.Unlock()
		}

		tm.mu.Lock()
		turn.Results = append(turn.Results, results...)
		tm.mu.Unlock()

		assistantMsg := api.Message{
			ID:        idgen.GenerateID(),
			Role:      api.RoleAssistant,
			Content:   turn.Response,
			ToolCalls: toolCalls,
			CreatedAt: time.Now().UTC(),
		}
		_ = tm.store.AppendMessage(ctx, sessionID, assistantMsg)

		for _, result := range results {
			toolContent := result.Output
			if result.Error != "" {
				toolContent = fmt.Sprintf("Error: %s", result.Error)
			}
			toolMsg := api.Message{
				ID:         idgen.GenerateID(),
				Role:       api.RoleTool,
				Content:    toolContent,
				ToolCallID: result.CallID,
				CreatedAt:  time.Now().UTC(),
			}
			_ = tm.store.AppendMessage(ctx, sessionID, toolMsg)
		}

		messages, err = tm.store.GetMessages(ctx, sessionID, msgLimit)
		if err != nil {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("get messages: %w", err))
			return
		}

		streamCh, err := tm.llm.ChatStream(ctx, messages, tools)
		if err != nil {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("chat stream: %w", err))
			return
		}

		content, toolCalls, err = tm.consumeStream(ctx, sessionID, turn, streamCh, outCh)
		if err != nil {
			return
		}

		tm.mu.Lock()
		turn.Response = content
		turn.ToolCalls = append(turn.ToolCalls, toolCalls...)
		if len(toolCalls) > 0 {
			turn.State = api.TurnToolCalls
		}
		tm.mu.Unlock()
	}

	assistantMsg := api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleAssistant,
		Content:   turn.Response,
		CreatedAt: time.Now().UTC(),
	}
	_ = tm.store.AppendMessage(ctx, sessionID, assistantMsg)

	tm.mu.Lock()
	turn.State = api.TurnIdle
	ended := time.Now().UTC()
	turn.EndedAt = &ended
	tm.mu.Unlock()
	if err := tm.saveTurn(ctx, sessionID, turn); err != nil {
		slog.Error("failed to save turn", "error", err)
	}
	tm.mu.Lock()
	tm.turn = turn
	tm.mu.Unlock()
}

// consumeStream reads chunks from a stream channel, forwards content to outCh,
// and returns the accumulated text plus any tool calls from the final chunk.
// It checks for context cancellation both during streaming and after the channel closes.
func (tm *TurnManager) consumeStream(ctx context.Context, sessionID string, turn *api.Turn, streamCh <-chan api.StreamChunk, outCh chan string) (string, []api.ToolCall, error) {
	var content strings.Builder
	var toolCalls []api.ToolCall

	for chunk := range streamCh {
		if ctx.Err() != nil {
			select {
			case outCh <- fmt.Sprintf("Error: %v", ctx.Err()):
			default:
			}
			tm.setError(ctx, sessionID, turn, ctx.Err())
			return "", nil, ctx.Err()
		}

		if chunk.Error != nil {
			select {
			case outCh <- fmt.Sprintf("Error: %v", chunk.Error):
			default:
			}
			tm.setError(ctx, sessionID, turn, chunk.Error)
			return "", nil, chunk.Error
		}

		if chunk.Content != "" {
			if content.Len()+len(chunk.Content) > maxStreamResponseSize {
				select {
				case outCh <- fmt.Sprintf("Error: response exceeded max size of %d bytes", maxStreamResponseSize):
				default:
				}
				tm.setError(ctx, sessionID, turn, fmt.Errorf("response exceeded max size of %d bytes", maxStreamResponseSize))
				return "", nil, fmt.Errorf("response exceeded max size of %d bytes", maxStreamResponseSize)
			}
			content.WriteString(chunk.Content)
			select {
			case outCh <- chunk.Content:
			case <-ctx.Done():
				select {
				case outCh <- fmt.Sprintf("Error: %v", ctx.Err()):
				default:
				}
				tm.setError(ctx, sessionID, turn, ctx.Err())
				return "", nil, ctx.Err()
			}
		}

		if chunk.Done {
			toolCalls = chunk.ToolCalls
			break
		}
	}

	if ctx.Err() != nil {
		select {
		case outCh <- fmt.Sprintf("Error: %v", ctx.Err()):
		default:
		}
		tm.setError(ctx, sessionID, turn, ctx.Err())
		return "", nil, ctx.Err()
	}

	return content.String(), toolCalls, nil
}

// executeToolCalls runs each tool call after checking the approval gate.
// It returns results for auto-approved/denied calls and a slice of pending calls
// that require manual approval.
func (tm *TurnManager) executeToolCalls(ctx context.Context, sessionID string, turn *api.Turn, calls []api.ToolCall) ([]api.ToolResult, []api.ToolCall) {
	results := make([]api.ToolResult, 0, len(calls))
	pending := make([]api.ToolCall, 0)

	for _, call := range calls {
		decision, autoApproved := tm.approval.ShouldAutoApprove(call)

		if !autoApproved {
			pending = append(pending, call)
			continue
		}

		if decision == api.ApprovalNo {
			results = append(results, api.ToolResult{
				CallID: call.ID,
				Name:   call.Name,
				Error:  "tool call denied",
			})
			continue
		}

		result, err := tm.tools.Execute(ctx, call)
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}

	if len(pending) > 0 {
		tm.mu.Lock()
		turn.State = api.TurnWaitingApproval
		tm.mu.Unlock()
		if err := tm.saveTurn(ctx, sessionID, turn); err != nil {
			slog.Error("failed to save turn", "error", err)
		}
		tm.pendingMu.Lock()
		tm.pendingCalls = append([]api.ToolCall(nil), pending...)
		tm.pendingMu.Unlock()
	}

	return results, pending
}

func (tm *TurnManager) setError(ctx context.Context, sessionID string, turn *api.Turn, err error) {
	tm.mu.Lock()
	turn.State = api.TurnError
	turn.Error = err.Error()
	ended := time.Now().UTC()
	turn.EndedAt = &ended
	tm.mu.Unlock()
	tm.pendingMu.Lock()
	tm.pendingCalls = nil
	tm.pendingMu.Unlock()
	if saveErr := tm.saveTurn(ctx, sessionID, turn); saveErr != nil {
		slog.Error("failed to save turn", "error", saveErr)
	}
	tm.mu.Lock()
	tm.turn = turn
	tm.mu.Unlock()
}

func (tm *TurnManager) saveTurn(ctx context.Context, sessionID string, turn *api.Turn) error {
	if err := tm.store.SaveTurn(ctx, sessionID, *turn); err != nil {
		// Best-effort persistence; do not fail the turn.
		return err
	}
	return nil
}
