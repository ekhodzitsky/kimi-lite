package core

import (
	"context"
	"errors"
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

// approvalPayload carries approval decisions together with the requestID
// they are intended for, preventing stale approvals from affecting the
// wrong tool round.
type approvalPayload struct {
	requestID int64
	decisions map[string]api.ApprovalDecision
}

// TurnManager orchestrates a single user input → LLM response cycle.
type TurnManager struct {
	llm              api.LLMClient
	tools            api.ToolExecutor
	approval         api.ApprovalGate
	store            api.Store
	cfg              api.ConfigProvider
	hookRunner       api.HookRunner
	metrics          api.MetricsCollector
	sandboxRoot      string
	protectedPaths   []string
	currentSessionID string
	mu               sync.RWMutex
	turn             *api.Turn

	pendingMu    sync.Mutex
	pendingCalls []api.ToolCall
	requestID    int64
	approvalCh   chan approvalPayload
	wg           sync.WaitGroup
	running      atomic.Bool

	cancelMu     sync.Mutex
	activeCancel context.CancelFunc
}

// NewTurnManager creates a new TurnManager. It returns an error if any required
// dependency (llm, tools, approval, store) is nil.
func NewTurnManager(llm api.LLMClient, tools api.ToolExecutor, approval api.ApprovalGate, store api.Store, cfg api.ConfigProvider) (*TurnManager, error) {
	if isNilInterface(llm) {
		return nil, fmt.Errorf("llm client is required")
	}
	if isNilInterface(tools) {
		return nil, fmt.Errorf("tool executor is required")
	}
	if isNilInterface(approval) {
		return nil, fmt.Errorf("approval gate is required")
	}
	if isNilInterface(store) {
		return nil, fmt.Errorf("store is required")
	}
	return &TurnManager{
		llm:        llm,
		tools:      tools,
		approval:   approval,
		store:      store,
		cfg:        cfg,
		metrics:    api.NoopMetricsCollector{},
		approvalCh: make(chan approvalPayload, 1),
	}, nil
}

// SetHookRunner sets the lifecycle hook runner.
func (tm *TurnManager) SetHookRunner(r api.HookRunner) {
	tm.hookRunner = r
}

// SetMetricsCollector sets the metrics collector.
func (tm *TurnManager) SetMetricsCollector(m api.MetricsCollector) {
	if m == nil {
		m = api.NoopMetricsCollector{}
	}
	tm.metrics = m
}

// SetSandboxRoot sets the root directory used for diff previews.
func (tm *TurnManager) SetSandboxRoot(root string) {
	tm.sandboxRoot = root
}

// SetProtectedPaths sets additional paths that must be blocked by diff previews.
// This mirrors BuiltInToolExecutor.protectedPaths.
func (tm *TurnManager) SetProtectedPaths(paths []string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.protectedPaths = paths
}

func (tm *TurnManager) getProtectedPaths() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.protectedPaths
}

// CurrentTurn returns a deep copy of the current turn.
func (tm *TurnManager) CurrentTurn() *api.Turn {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.turn == nil {
		return nil
	}
	t := *tm.turn
	if len(tm.turn.ToolCalls) > 0 {
		t.ToolCalls = make([]api.ToolCall, len(tm.turn.ToolCalls))
		copy(t.ToolCalls, tm.turn.ToolCalls)
	}
	if len(tm.turn.Results) > 0 {
		t.Results = make([]api.ToolResult, len(tm.turn.Results))
		copy(t.Results, tm.turn.Results)
	}
	return &t
}

// PendingApprovals returns a copy of the currently pending tool calls and the
// requestID for the round they belong to. A zero requestID means no approvals
// are currently pending.
func (tm *TurnManager) PendingApprovals() ([]api.ToolCall, int64) {
	tm.pendingMu.Lock()
	defer tm.pendingMu.Unlock()
	return append([]api.ToolCall(nil), tm.pendingCalls...), tm.requestID
}

// ResumeWithApproval resumes a turn that is waiting for manual approval.
// The requestID must match the current pending round and the sessionID must
// match the turn that created the pending calls; otherwise the request is rejected.
func (tm *TurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	tm.mu.RLock()
	turn := tm.turn
	currentSessionID := tm.currentSessionID
	tm.mu.RUnlock()
	if turn == nil {
		return fmt.Errorf("no active turn")
	}
	if currentSessionID != sessionID {
		return fmt.Errorf("sessionID mismatch: got %q, want %q", sessionID, currentSessionID)
	}

	tm.pendingMu.Lock()
	defer tm.pendingMu.Unlock()
	if len(tm.pendingCalls) == 0 {
		return fmt.Errorf("no pending approvals")
	}
	if tm.requestID != requestID {
		return fmt.Errorf("requestID mismatch: got %d, want %d", requestID, tm.requestID)
	}

	payload := approvalPayload{requestID: requestID, decisions: approvals}
	select {
	case tm.approvalCh <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("approval channel busy")
	}
}

// Wait blocks until all in-flight turns complete.
func (tm *TurnManager) Wait() {
	tm.wg.Wait()
}

// CancelAll cancels the currently in-flight turn, if any.
func (tm *TurnManager) CancelAll() {
	tm.cancelMu.Lock()
	cancel := tm.activeCancel
	tm.cancelMu.Unlock()
	if cancel != nil {
		tm.mu.RLock()
		sessionID := tm.currentSessionID
		turnID := ""
		if tm.turn != nil {
			turnID = tm.turn.ID
		}
		tm.mu.RUnlock()
		cancel()
		tm.runHooks(context.Background(), api.HookTurnInterrupt, sessionID, turnID, "")
	}
}

// RunTurn executes a complete turn for the given session and user input.
// It returns a channel that streams turn events (content, done, error).
// Returns an error if a turn is already in progress.
func (tm *TurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	if !tm.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("turn already in progress")
	}
	runCtx, runCancel := context.WithCancel(ctx)
	tm.cancelMu.Lock()
	tm.activeCancel = runCancel
	tm.cancelMu.Unlock()
	outCh, err := tm.startTurn(runCtx, runCancel, sessionID, input)
	if err != nil {
		tm.running.Store(false)
		runCancel()
		tm.cancelMu.Lock()
		tm.activeCancel = nil
		tm.cancelMu.Unlock()
		return nil, err
	}
	return outCh, nil
}

func (tm *TurnManager) startTurn(ctx context.Context, runCancel context.CancelFunc, sessionID string, input string) (<-chan api.TurnEvent, error) {
	if tm.cfg != nil && tm.cfg.Get() != nil {
		maxTurns := tm.cfg.Get().Behavior.MaxTurns
		if maxTurns > 0 {
			count, err := tm.store.CountTurns(ctx, sessionID, api.TurnIdle)
			if err != nil {
				return nil, fmt.Errorf("count turns: %w", err)
			}
			if count >= maxTurns {
				return nil, fmt.Errorf("max turns (%d) reached for session %s", maxTurns, sessionID)
			}
		}
	}

	tm.metrics.IncCounter("turn.started")
	tm.runHooks(ctx, api.HookTurnStart, sessionID, "", "")

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
		State:     api.TurnStreaming,
		Input:     input,
		StartedAt: time.Now().UTC(),
	}

	tm.mu.Lock()
	tm.turn = turn
	tm.currentSessionID = sessionID
	tm.mu.Unlock()

	// Persistence policy: turn-save failure is fatal at turn start...
	if err := tm.store.SaveTurn(ctx, sessionID, *turn); err != nil {
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

	tools := tm.tools.Definitions(ctx)

	// Start the first LLM stream synchronously so that immediate errors are returned.
	eventCh := make(chan api.TurnEvent, 16)
	streamCh, err := tm.llm.ChatStream(ctx, messages, tools)
	if err != nil {
		tm.setError(ctx, sessionID, turn, fmt.Errorf("chat stream: %w", err), nil)
		return nil, fmt.Errorf("chat stream: %w", err)
	}

	tm.wg.Add(1)
	go tm.run(ctx, runCancel, sessionID, turn, tools, streamCh, eventCh, msgLimit)

	return eventCh, nil
}

func (tm *TurnManager) run(ctx context.Context, runCancel context.CancelFunc, sessionID string, turn *api.Turn, tools []api.ToolDefinition, firstStream <-chan api.StreamChunk, eventCh chan api.TurnEvent, msgLimit int) {
	defer tm.wg.Done()
	defer close(eventCh)
	defer tm.running.Store(false)
	defer runCancel()
	defer func() {
		tm.cancelMu.Lock()
		tm.activeCancel = nil
		tm.cancelMu.Unlock()
	}()

	content, toolCalls, err := tm.consumeStream(ctx, sessionID, turn, firstStream, eventCh)
	if err != nil {
		tm.persistPartialResponse(ctx, sessionID, turn, content)
		tm.setError(ctx, sessionID, turn, err, eventCh)
		return
	}

	tm.mu.Lock()
	turn.Response = content
	turn.ToolCalls = toolCalls
	if len(toolCalls) > 0 {
		turn.State = api.TurnToolCalls
	}
	tm.mu.Unlock()
	if err := tm.store.SaveTurn(ctx, sessionID, *turn); err != nil {
		slog.Error("failed to save turn", "error", err)
	}

	maxToolRounds := 10
	if tm.cfg != nil && tm.cfg.Get() != nil {
		if v := tm.cfg.Get().Behavior.MaxToolRounds; v > 0 {
			maxToolRounds = v
		}
	}
	for round := 0; len(toolCalls) > 0; round++ {
		if round >= maxToolRounds {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("max tool rounds (%d) exceeded", maxToolRounds), eventCh)
			return
		}
		results, pending, pendingIdx := tm.executeToolCalls(ctx, sessionID, turn, toolCalls)

		if len(pending) > 0 {
			_, reqID := tm.PendingApprovals()
			tm.runApprovalHook(ctx, api.HookApprovalRequest, sessionID, turn.ID, pending)
			select {
			case eventCh <- api.TurnEvent{Type: api.TurnEventApprovalRequest, ToolCalls: pending, RequestID: reqID}:
			case <-ctx.Done():
				tm.setError(ctx, sessionID, turn, ctx.Err(), eventCh)
				return
			}

		approvalLoop:
			for {
				var payload approvalPayload
				select {
				case payload = <-tm.approvalCh:
				case <-ctx.Done():
					tm.setError(ctx, sessionID, turn, ctx.Err(), eventCh)
					return
				}

				// Ignore stale approval payloads whose requestID does not match
				// the current pending set.
				tm.pendingMu.Lock()
				currentID := tm.requestID
				tm.pendingMu.Unlock()
				if payload.requestID != currentID {
					slog.Warn("ignoring stale approval payload", "got", payload.requestID, "want", currentID)
					// Treat all pending calls as denied at their original indices.
					for j, call := range pending {
						results[pendingIdx[j]] = api.ToolResult{
							CallID: call.ID,
							Name:   call.Name,
							Error:  "tool call denied (stale approval)",
						}
					}
					break approvalLoop
				}

				// Determine whether every decision in the payload is a diff request.
				diffOnly := len(payload.decisions) > 0
				for _, decision := range payload.decisions {
					if decision != api.ApprovalDiff {
						diffOnly = false
						break
					}
				}

				if diffOnly {
					for callID, decision := range payload.decisions {
						if decision != api.ApprovalDiff {
							continue
						}
						for _, call := range pending {
							if call.ID != callID {
								continue
							}
							diffContent, diffErr := ToolCallDiff(call, tm.sandboxRoot, tm.getProtectedPaths())
							if diffErr != nil {
								slog.Debug("diff preview failed", "tool", call.Name, "error", diffErr)
							}
							select {
							case eventCh <- api.TurnEvent{
								Type:        api.TurnEventApprovalDiff,
								DiffCallID:  call.ID,
								DiffContent: diffContent,
							}:
							case <-ctx.Done():
								tm.setError(ctx, sessionID, turn, ctx.Err(), eventCh)
								return
							}
						}
					}
					// Keep the same pending calls and requestID; wait for a final decision.
					continue approvalLoop
				}

				for j, call := range pending {
					if err := ctx.Err(); err != nil {
						results[pendingIdx[j]] = api.ToolResult{
							CallID: call.ID,
							Name:   call.Name,
							Error:  fmt.Sprintf("context cancelled: %v", err),
						}
						continue
					}
					decision, ok := payload.decisions[call.ID]
					if !ok {
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
						result, err = tm.executeToolCall(ctx, call)
						if err != nil {
							result.Error = err.Error()
						}
					}
					if result.CallID == "" {
						result.CallID = call.ID
					}
					if result.Name == "" {
						result.Name = call.Name
					}
					results[pendingIdx[j]] = result
				}
				break approvalLoop
			}

			tm.runApprovalHook(ctx, api.HookApprovalDecision, sessionID, turn.ID, pending)

			tm.pendingMu.Lock()
			tm.pendingCalls = nil
			tm.pendingMu.Unlock()
		}

		tm.mu.Lock()
		turn.Results = append(turn.Results, results...)
		tm.mu.Unlock()

		// Emit tool result events so the TUI can display them.
		for _, result := range results {
			select {
			case eventCh <- api.TurnEvent{Type: api.TurnEventToolResult, Result: result}:
			case <-ctx.Done():
				tm.setError(ctx, sessionID, turn, ctx.Err(), eventCh)
				return
			}
		}

		tm.mu.RLock()
		assistantContent := turn.Response
		tm.mu.RUnlock()
		assistantMsg := api.Message{
			ID:        idgen.GenerateID(),
			Role:      api.RoleAssistant,
			Content:   assistantContent,
			ToolCalls: toolCalls,
			CreatedAt: time.Now().UTC(),
		}
		if err := tm.store.AppendMessage(ctx, sessionID, assistantMsg); err != nil {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("append message: %w", err), eventCh)
			return
		}

		for _, result := range results {
			toolContent := result.Output
			if result.Error != "" {
				if result.Output != "" {
					toolContent = result.Output + "\nError: " + result.Error
				} else {
					toolContent = fmt.Sprintf("Error: %s", result.Error)
				}
			}
			if toolContent == "" && len(result.ContentParts) > 0 {
				toolContent = "[tool output contains non-text content]"
			}
			toolMsg := api.Message{
				ID:           idgen.GenerateID(),
				Role:         api.RoleTool,
				Content:      toolContent,
				ContentParts: result.ContentParts,
				ToolCallID:   result.CallID,
				CreatedAt:    time.Now().UTC(),
			}
			if err := tm.store.AppendMessage(ctx, sessionID, toolMsg); err != nil {
				tm.setError(ctx, sessionID, turn, fmt.Errorf("append message: %w", err), eventCh)
				return
			}
		}

		messages, err := tm.store.GetMessages(ctx, sessionID, msgLimit)
		if err != nil {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("get messages: %w", err), eventCh)
			return
		}

		streamCh, err := tm.llm.ChatStream(ctx, messages, tools)
		if err != nil {
			tm.setError(ctx, sessionID, turn, fmt.Errorf("chat stream: %w", err), eventCh)
			return
		}

		content, toolCalls, err = tm.consumeStream(ctx, sessionID, turn, streamCh, eventCh)
		if err != nil {
			tm.persistPartialResponse(ctx, sessionID, turn, content)
			tm.setError(ctx, sessionID, turn, err, eventCh)
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

	select {
	case eventCh <- api.TurnEvent{Type: api.TurnEventDone, ToolCalls: toolCalls}:
	case <-ctx.Done():
	}

	// Skip persisting an empty trailing assistant message.
	tm.mu.RLock()
	finalResponse := turn.Response
	tm.mu.RUnlock()
	if strings.TrimSpace(finalResponse) != "" {
		assistantMsg := api.Message{
			ID:        idgen.GenerateID(),
			Role:      api.RoleAssistant,
			Content:   finalResponse,
			CreatedAt: time.Now().UTC(),
		}
		if err := tm.store.AppendMessage(ctx, sessionID, assistantMsg); err != nil {
			slog.Error("failed to append message", "error", err)
		}
	}

	tm.mu.Lock()
	turn.State = api.TurnIdle
	ended := time.Now().UTC()
	turn.EndedAt = &ended
	tm.mu.Unlock()
	if err := tm.store.SaveTurn(ctx, sessionID, *turn); err != nil {
		slog.Error("failed to save turn", "error", err)
	}
	tm.metrics.IncCounter("turn.completed")
	tm.runHooks(ctx, api.HookTurnEnd, sessionID, turn.ID, "")
	tm.mu.Lock()
	tm.turn = turn
	tm.mu.Unlock()
}

// consumeStream reads chunks from a stream channel, forwards content as TurnEvents,
// and returns the accumulated text plus any tool calls from the final chunk.
// It is interruptible via ctx.Done() and drains any remaining streamCh items
// before returning an error so the producer goroutine can exit cleanly.
func (tm *TurnManager) consumeStream(ctx context.Context, _ string, _ *api.Turn, streamCh <-chan api.StreamChunk, eventCh chan api.TurnEvent) (string, []api.ToolCall, error) {
	var content strings.Builder
	var toolCalls []api.ToolCall

	drain := func() {
		for range streamCh {
			continue // drain remaining chunks so the producer can unblock and exit
		}
	}

	for {
		select {
		case <-ctx.Done():
			drain()
			select {
			case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: ctx.Err()}:
			case <-ctx.Done():
			}
			return content.String(), nil, ctx.Err()
		case chunk, ok := <-streamCh:
			if !ok {
				if err := ctx.Err(); err != nil {
					select {
					case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: err}:
					case <-ctx.Done():
					}
					return content.String(), nil, err
				}
				return content.String(), toolCalls, nil
			}

			if chunk.Error != nil {
				select {
				case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: chunk.Error}:
				case <-ctx.Done():
				}
				drain()
				return content.String(), nil, chunk.Error
			}

			if chunk.Content != "" {
				if content.Len()+len(chunk.Content) > maxStreamResponseSize {
					msg := fmt.Sprintf("response exceeded max size of %d bytes", maxStreamResponseSize)
					select {
					case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: errors.New(msg)}:
					case <-ctx.Done():
					}
					drain()
					return content.String(), nil, errors.New(msg)
				}
				content.WriteString(chunk.Content)
				select {
				case eventCh <- api.TurnEvent{Type: api.TurnEventContent, Content: chunk.Content}:
				case <-ctx.Done():
					select {
					case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: ctx.Err()}:
					case <-ctx.Done():
					}
					drain()
					return content.String(), nil, ctx.Err()
				}
			}

			if chunk.Done {
				toolCalls = chunk.ToolCalls
				if err := ctx.Err(); err != nil {
					drain()
					select {
					case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: err}:
					case <-ctx.Done():
					}
					return content.String(), nil, err
				}
				return content.String(), toolCalls, nil
			}
		}
	}
}

// executeToolCalls runs each tool call after checking the approval gate.
// It returns a result slice with placeholders for pending calls, the pending
// calls, and the indices at which pending-call results must be inserted to
// preserve the original call order.
func (tm *TurnManager) executeToolCalls(ctx context.Context, sessionID string, turn *api.Turn, calls []api.ToolCall) ([]api.ToolResult, []api.ToolCall, []int) {
	results := make([]api.ToolResult, len(calls))
	pending := make([]api.ToolCall, 0)
	pendingIdx := make([]int, 0)

	for i, call := range calls {
		if err := ctx.Err(); err != nil {
			results[i] = api.ToolResult{
				CallID: call.ID,
				Name:   call.Name,
				Error:  fmt.Sprintf("context cancelled: %v", err),
			}
			continue
		}

		decision, autoApproved := tm.approval.ShouldAutoApprove(call)

		if !autoApproved {
			pending = append(pending, call)
			pendingIdx = append(pendingIdx, i)
			continue
		}

		if decision == api.ApprovalNo {
			results[i] = api.ToolResult{
				CallID: call.ID,
				Name:   call.Name,
				Error:  "tool call denied",
			}
			continue
		}

		result, err := tm.executeToolCall(ctx, call)
		if err != nil {
			result.Error = err.Error()
		}
		if result.CallID == "" {
			result.CallID = call.ID
		}
		if result.Name == "" {
			result.Name = call.Name
		}
		results[i] = result
	}

	if len(pending) > 0 {
		tm.mu.Lock()
		turn.State = api.TurnWaitingApproval
		tm.mu.Unlock()
		if err := tm.store.SaveTurn(ctx, sessionID, *turn); err != nil {
			slog.Error("failed to save turn", "error", err)
		}
		tm.pendingMu.Lock()
		tm.pendingCalls = append([]api.ToolCall(nil), pending...)
		tm.requestID++
		tm.pendingMu.Unlock()
	}

	return results, pending, pendingIdx
}

// executeToolCall runs a single tool call and recovers from panics, converting
// them into a ToolResult with an error message so one bad tool cannot crash the
// whole turn.
func (tm *TurnManager) executeToolCall(ctx context.Context, call api.ToolCall) (result api.ToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = api.ToolResult{
				CallID: call.ID,
				Name:   call.Name,
				Error:  fmt.Sprintf("tool execution panicked: %v", r),
			}
			err = nil
		}
	}()
	return tm.tools.Execute(ctx, call)
}

func (tm *TurnManager) setError(ctx context.Context, sessionID string, turn *api.Turn, err error, eventCh chan api.TurnEvent) {
	tm.metrics.IncCounter("turn.errored")
	tm.mu.Lock()
	turn.State = api.TurnError
	turn.Error = err.Error()
	ended := time.Now().UTC()
	turn.EndedAt = &ended
	tm.mu.Unlock()
	tm.pendingMu.Lock()
	tm.pendingCalls = nil
	tm.pendingMu.Unlock()
	if saveErr := tm.store.SaveTurn(ctx, sessionID, *turn); saveErr != nil {
		slog.Error("failed to save turn", "error", saveErr)
	}
	if eventCh != nil {
		select {
		case eventCh <- api.TurnEvent{Type: api.TurnEventError, Error: err}:
		case <-ctx.Done():
		}
	}
	tm.mu.Lock()
	tm.turn = turn
	tm.mu.Unlock()
}

func (tm *TurnManager) persistPartialResponse(ctx context.Context, sessionID string, turn *api.Turn, content string) {
	tm.mu.Lock()
	turn.Response = content
	tm.mu.Unlock()
	if content == "" {
		return
	}
	assistantMsg := api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleAssistant,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if err := tm.store.AppendMessage(ctx, sessionID, assistantMsg); err != nil {
		slog.Error("failed to append partial assistant message", "error", err)
	}
}

func (tm *TurnManager) runHooks(ctx context.Context, event api.HookEvent, sessionID, turnID, input string) {
	if ctx.Err() != nil {
		return
	}
	if tm.hookRunner == nil {
		return
	}
	if err := tm.hookRunner.Run(ctx, api.HookData{
		Event:     event,
		SessionID: sessionID,
		TurnID:    turnID,
		ToolArgs:  input,
	}); err != nil {
		slog.Warn("turn hook failed", "event", event, "error", err)
	}
}

func (tm *TurnManager) runApprovalHook(ctx context.Context, event api.HookEvent, sessionID, turnID string, calls []api.ToolCall) {
	if ctx.Err() != nil {
		return
	}
	if tm.hookRunner == nil || len(calls) == 0 {
		return
	}
	for _, call := range calls {
		if err := tm.hookRunner.Run(ctx, api.HookData{
			Event:     event,
			SessionID: sessionID,
			TurnID:    turnID,
			ToolName:  call.Name,
			ToolArgs:  call.Arguments,
		}); err != nil {
			slog.Warn("approval hook failed", "event", event, "tool", call.Name, "error", err)
		}
	}
}
