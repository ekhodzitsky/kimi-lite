package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// --- TurnManager setters and lifecycle ---

func TestTurnManager_Setters(t *testing.T) {
	t.Parallel()
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	// Nil metrics should be replaced with noop.
	tm.SetMetricsCollector(nil)
	if tm.metrics == nil {
		t.Error("metrics should not be nil after SetMetricsCollector(nil)")
	}

	tm.SetHookRunner(&recordingHookRunner{})
	if tm.hookRunner == nil {
		t.Error("hook runner should be set")
	}

	tm.SetProtectedPaths([]string{"/secret"})
	if got := tm.getProtectedPaths(); len(got) != 1 || got[0] != "/secret" {
		t.Errorf("protected paths = %v, want [/secret]", got)
	}
}

func TestNewTurnManager_RequiresDependencies(t *testing.T) {
	t.Parallel()
	store := newMockStore()
	llm := &mockLLMClient{}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{}

	if _, err := NewTurnManager(nil, tools, approval, store, nil); err == nil {
		t.Error("expected error for nil llm")
	}
	if _, err := NewTurnManager(llm, nil, approval, store, nil); err == nil {
		t.Error("expected error for nil tools")
	}
	if _, err := NewTurnManager(llm, tools, nil, store, nil); err == nil {
		t.Error("expected error for nil approval")
	}
	if _, err := NewTurnManager(llm, tools, approval, nil, nil); err == nil {
		t.Error("expected error for nil store")
	}
}

func TestTurnManager_Wait(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "hello"},
			api.StreamChunk{Done: true},
		),
	}
	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	// Drain all events before waiting so the channel is empty when we check
	// whether it has been closed.
	for range outCh {
	}
	tm.Wait()
	if _, open := <-outCh; open {
		t.Error("expected output channel to be closed after Wait")
	}
}

func TestTurnManager_CancelAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				select {
				case ch <- api.StreamChunk{Content: "partial"}:
				case <-ctx.Done():
					return
				}
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					return
				}
				ch <- api.StreamChunk{Done: true}
			}()
			return ch, nil
		},
	}
	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// Give the stream a moment to start, then cancel and drain events.
	time.Sleep(50 * time.Millisecond)
	tm.CancelAll()
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected turn error state, got %v", turn)
	}
}

func TestTurnManager_CancelAll_NoActiveTurn(t *testing.T) {
	t.Parallel()
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)
	// Should not panic when there is no active turn.
	tm.CancelAll()
}

// --- ResumeWithApproval error paths ---

func TestTurnManager_ResumeWithApproval_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)

	if err := tm.ResumeWithApproval(ctx, sess.ID, 1, nil); err == nil {
		t.Error("expected error when no active turn")
	}

	tm.turn = &api.Turn{ID: "t1"}
	tm.currentSessionID = sess.ID
	if err := tm.ResumeWithApproval(ctx, "wrong-session", 1, nil); err == nil {
		t.Error("expected error for session ID mismatch")
	}

	if err := tm.ResumeWithApproval(ctx, sess.ID, 1, nil); err == nil {
		t.Error("expected error when no pending approvals")
	}

	tm.pendingMu.Lock()
	tm.pendingCalls = []api.ToolCall{{ID: "tc1", Name: "write_file"}}
	tm.requestID = 5
	tm.pendingMu.Unlock()

	if err := tm.ResumeWithApproval(ctx, sess.ID, 4, nil); err == nil {
		t.Error("expected error for requestID mismatch")
	}

	// Fill the approval channel to force the busy path.
	tm.approvalCh <- approvalPayload{requestID: 5}
	if err := tm.ResumeWithApproval(ctx, sess.ID, 5, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err == nil {
		t.Error("expected error for busy approval channel")
	} else if !strings.Contains(err.Error(), "busy") {
		t.Errorf("error = %q, want busy", err.Error())
	}
}

func TestTurnManager_ResumeWithApproval_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)
	tm.turn = &api.Turn{ID: "t1"}
	tm.currentSessionID = sess.ID
	tm.pendingMu.Lock()
	tm.pendingCalls = []api.ToolCall{{ID: "tc1", Name: "write_file"}}
	tm.requestID = 1
	tm.pendingMu.Unlock()

	// Fill the channel so the send blocks and ctx.Done() is selected.
	tm.approvalCh <- approvalPayload{requestID: 1}

	if err := tm.ResumeWithApproval(ctx, sess.ID, 1, nil); err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// --- Hooks ---

func TestTurnManager_RunHooks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hook := &recordingHookRunner{}
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)
	tm.SetHookRunner(hook)

	// Nil runner should not panic.
	tm.SetHookRunner(nil)
	tm.runHooks(ctx, api.HookTurnStart, "sess", "turn", "input")

	// Runner that returns an error should be swallowed.
	tm.SetHookRunner(&errorHookRunner{})
	tm.runHooks(ctx, api.HookTurnStart, "sess", "turn", "input")

	// Cancelled context should short-circuit.
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	tm.SetHookRunner(hook)
	tm.runHooks(cancelledCtx, api.HookTurnStart, "sess", "turn", "input")
}

func TestTurnManager_RunApprovalHook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hook := &recordingHookRunner{}
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)
	tm.SetHookRunner(hook)

	// Empty calls should be a no-op.
	tm.runApprovalHook(ctx, api.HookApprovalRequest, "sess", "turn", nil)

	// Nil runner with calls should be a no-op.
	tm.SetHookRunner(nil)
	tm.runApprovalHook(ctx, api.HookApprovalRequest, "sess", "turn", []api.ToolCall{{ID: "tc1", Name: "write_file"}})

	// Runner that returns an error should be swallowed.
	tm.SetHookRunner(&errorHookRunner{})
	tm.runApprovalHook(ctx, api.HookApprovalRequest, "sess", "turn", []api.ToolCall{{ID: "tc1", Name: "write_file"}})

	// Cancelled context should short-circuit.
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	tm.SetHookRunner(hook)
	tm.runApprovalHook(cancelledCtx, api.HookApprovalRequest, "sess", "turn", []api.ToolCall{{ID: "tc1", Name: "write_file"}})
}

type errorHookRunner struct{}

func (e *errorHookRunner) Run(_ context.Context, _ api.HookData) error {
	return fmt.Errorf("hook failed")
}

// --- consumeStream branches ---

func TestTurnManager_ConsumeStream_MaxSizeExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	large := strings.Repeat("a", maxStreamResponseSize+1)
	streamCh := make(chan api.StreamChunk, 2)
	streamCh <- api.StreamChunk{Content: large}
	close(streamCh)

	eventCh := make(chan api.TurnEvent, 4)
	content, _, err := tm.consumeStream(ctx, "sess", &api.Turn{}, streamCh, eventCh)
	if err == nil {
		t.Fatal("expected error for max size exceeded")
	}
	if !strings.Contains(err.Error(), "max size") {
		t.Errorf("error = %q, want max size", err.Error())
	}
	if content != "" {
		// The oversized chunk is rejected before being appended, so the
		// accumulated content remains empty.
		t.Errorf("content length = %d, want 0", len(content))
	}
}

func TestTurnManager_ConsumeStream_ContextCancelledAfterClose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	streamCh := make(chan api.StreamChunk, 1)
	streamCh <- api.StreamChunk{Content: "hello", Done: true}
	close(streamCh)

	// Cancel after the channel has already closed.
	cancel()

	eventCh := make(chan api.TurnEvent, 4)
	_, _, err := tm.consumeStream(ctx, "sess", &api.Turn{}, streamCh, eventCh)
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// --- persistPartialResponse ---

func TestTurnManager_PersistPartialResponse_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)
	turn := &api.Turn{ID: "t1"}
	tm.persistPartialResponse(ctx, sess.ID, turn, "")

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 0 {
		t.Errorf("expected no messages, got %d", len(msgs))
	}
}

// --- setError branches ---

func TestTurnManager_SetError_NilEventCh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)
	turn := &api.Turn{ID: "t1", State: api.TurnStreaming}
	tm.setError(ctx, sess.ID, turn, fmt.Errorf("boom"), nil)

	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
}

func TestTurnManager_SetError_SaveTurnFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingSaveStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)
	turn := &api.Turn{ID: "t1", State: api.TurnStreaming}
	eventCh := make(chan api.TurnEvent, 2)
	tm.setError(ctx, sess.ID, turn, fmt.Errorf("boom"), eventCh)

	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
}

type failingSaveStore struct {
	*mockStore
}

func (m *failingSaveStore) SaveTurn(_ context.Context, _ string, _ api.Turn) error {
	return fmt.Errorf("save turn failed")
}

// --- startTurn error branches ---

func TestTurnManager_StartTurn_CountTurnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &countTurnsErrorStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}
	tm := newTestTurnManager(t, nil, nil, nil, store, cfg)

	_, err := tm.startTurn(ctx, func() {}, sess.ID, "hi")
	if err == nil {
		t.Fatal("expected error for CountTurns failure")
	}
	if !strings.Contains(err.Error(), "count turns") {
		t.Errorf("error = %q, want count turns", err.Error())
	}
}

type countTurnsErrorStore struct {
	*mockStore
}

func (m *countTurnsErrorStore) CountTurns(_ context.Context, _ string, _ api.TurnState) (int, error) {
	return 0, fmt.Errorf("count turns failed")
}

func TestTurnManager_StartTurn_SaveTurnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingSaveStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)

	_, err := tm.startTurn(ctx, func() {}, sess.ID, "hi")
	if err == nil {
		t.Fatal("expected error for SaveTurn failure")
	}
	if !strings.Contains(err.Error(), "save turn") {
		t.Errorf("error = %q, want save turn", err.Error())
	}
}

func TestTurnManager_StartTurn_AppendMessageError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingAppendStore{mockStore: newMockStore(), failAfter: 0}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)

	_, err := tm.startTurn(ctx, func() {}, sess.ID, "hi")
	if err == nil {
		t.Fatal("expected error for AppendMessage failure")
	}
	if !strings.Contains(err.Error(), "append user message") {
		t.Errorf("error = %q, want append user message", err.Error())
	}
}

func TestTurnManager_StartTurn_GetMessagesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &getMessagesErrorStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)

	_, err := tm.startTurn(ctx, func() {}, sess.ID, "hi")
	if err == nil {
		t.Fatal("expected error for GetMessages failure")
	}
	if !strings.Contains(err.Error(), "get messages") {
		t.Errorf("error = %q, want get messages", err.Error())
	}
}

type getMessagesErrorStore struct {
	*mockStore
}

func (m *getMessagesErrorStore) GetMessages(_ context.Context, _ string, _ int) ([]api.Message, error) {
	return nil, fmt.Errorf("get messages failed")
}

func TestTurnManager_StartTurn_ChatStreamError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(_ context.Context, _ []api.Message, _ []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return nil, fmt.Errorf("chat stream failed")
		},
	}
	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, nil, store, nil)

	_, err := tm.startTurn(ctx, func() {}, sess.ID, "hi")
	if err == nil {
		t.Fatal("expected error for ChatStream failure")
	}
	if !strings.Contains(err.Error(), "chat stream") {
		t.Errorf("error = %q, want chat stream", err.Error())
	}
}

// --- run branches ---

type failingToolAppendStore struct {
	*mockStore
}

func (m *failingToolAppendStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if msg.Role == api.RoleTool {
		return fmt.Errorf("injected tool message append failure")
	}
	return m.mockStore.AppendMessage(ctx, sessionID, msg)
}

func TestTurnManager_Run_AppendToolMessageFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingToolAppendStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "append message") {
		t.Errorf("error = %q, want append message", turn.Error)
	}
}

func TestTurnManager_Run_GetMessagesAfterToolFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	baseStore := newMockStore()
	store := &getMessagesAfterErrorStore{mockStore: baseStore}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "get messages") {
		t.Errorf("error = %q, want get messages", turn.Error)
	}
}

type getMessagesAfterErrorStore struct {
	*mockStore
	calls int
}

func (m *getMessagesAfterErrorStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	m.calls++
	if m.calls >= 2 {
		return nil, fmt.Errorf("get messages failed")
	}
	return m.mockStore.GetMessages(ctx, sessionID, limit)
}

func TestTurnManager_Run_ChatStreamAfterToolFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return nil, fmt.Errorf("chat stream failed")
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "chat stream") {
		t.Errorf("error = %q, want chat stream", turn.Error)
	}
}

// --- executeToolCalls branches ---

func TestTurnManager_ExecuteToolCalls_ApprovalNo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, _ api.ToolCall) (api.ToolResult, error) {
			t.Error("Execute should not be called for ApprovalNo")
			return api.ToolResult{}, nil
		},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, true }}
	tm := newTestTurnManager(t, nil, tools, approval, store, nil)

	results, pending, _ := tm.executeToolCalls(ctx, sess.ID, &api.Turn{ID: "t1"}, []api.ToolCall{{ID: "tc1", Name: "write_file"}}, nil)
	if len(pending) != 0 {
		t.Errorf("expected no pending, got %d", len(pending))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "tool call denied" {
		t.Errorf("error = %q, want tool call denied", results[0].Error)
	}
}

func TestTurnManager_ExecuteToolCalls_ExecuteError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, _ api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{}, fmt.Errorf("execution failed")
		},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, nil, tools, approval, store, nil)

	results, _, _ := tm.executeToolCalls(ctx, sess.ID, &api.Turn{ID: "t1"}, []api.ToolCall{{ID: "tc1", Name: "write_file"}}, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Error, "execution failed") {
		t.Errorf("error = %q, want execution failed", results[0].Error)
	}
}

func TestTurnManager_ExecuteToolCalls_PendingSaveTurnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingSaveStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, false }}
	tm := newTestTurnManager(t, nil, nil, approval, store, nil)

	turn := &api.Turn{ID: "t1", State: api.TurnToolCalls}
	results, pending, _ := tm.executeToolCalls(ctx, sess.ID, turn, []api.ToolCall{{ID: "tc1", Name: "write_file"}}, nil)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result placeholder, got %d", len(results))
	}
	// SaveTurn error is logged but pending state is still updated.
	if turn.State != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", turn.State)
	}
}

func TestTurnManager_Run_ApprovalProcessing_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	started := make(chan struct{})
	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
				{ID: "tc1", Name: "read_file", Arguments: `{}`},
				{ID: "tc2", Name: "read_file", Arguments: `{}`},
			}},
		),
	}
	tools := &mockToolExecutor{
		executeFunc: func(execCtx context.Context, call api.ToolCall) (api.ToolResult, error) {
			if call.ID == "tc1" {
				close(started)
				// Wait until the context is canceled before returning so the
				// second pending call sees ctx.Err() in the approval loop.
				<-execCtx.Done()
				return api.ToolResult{}, execCtx.Err()
			}
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, false },
	}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// Wait for the approval request, then approve and cancel while the first
	// tool call is still blocked.
	for e := range outCh {
		if e.Type == api.TurnEventApprovalRequest {
			_, reqID := tm.PendingApprovals()
			if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{
				"tc1": api.ApprovalYes,
				"tc2": api.ApprovalYes,
			}); err != nil {
				t.Fatalf("resume with approval: %v", err)
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for first tool call to start")
			}
			cancel()
			break
		}
	}

	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "context canceled") && !strings.Contains(turn.Error, "context cancelled") {
		t.Errorf("error = %q, want context canceled", turn.Error)
	}
}

// --- SessionManager paths ---

func TestPortablePath_RelativeError(t *testing.T) {
	t.Parallel()
	// filepath.Rel returns an error when given an impossible pair.
	if got := makePortablePath("/tmp/proj"); got != "/tmp/proj" {
		// The real implementation only errors when paths are on different
		// volumes on Windows; on Unix this just round-trips. We still assert
		// the behavior matches expectations.
		t.Errorf("makePortablePath = %q, want /tmp/proj", got)
	}
}

func TestPortablePath_HomeAndSibling(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory:", err)
	}

	if got := makePortablePath(home); got != "~" {
		t.Errorf("makePortablePath(home) = %q, want ~", got)
	}
	if got := resolvePortablePath("~"); got != home {
		t.Errorf("resolvePortablePath(~) = %q, want %q", got, home)
	}

	// A sibling directory that shares a prefix with home must not be treated
	// as inside home.
	sibling := home + "data"
	if got := makePortablePath(sibling); got != sibling {
		t.Errorf("sibling path %q incorrectly portable-ized as %q", sibling, got)
	}
}

func TestSessionManager_SetMetricsCollector_Nil(t *testing.T) {
	t.Parallel()
	sm := NewSessionManager(nil)
	sm.SetMetricsCollector(nil)
	if sm.metrics == nil {
		t.Error("metrics should not be nil after SetMetricsCollector(nil)")
	}
}

func TestSessionManager_Start_StoreError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &createSessionErrorStore{mockStore: newMockStore()}
	sm := NewSessionManager(store)

	if _, err := sm.Start(ctx, "/tmp/proj"); err == nil {
		t.Fatal("expected error for CreateSession failure")
	}
}

type createSessionErrorStore struct {
	*mockStore
}

func (m *createSessionErrorStore) CreateSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, fmt.Errorf("create session failed")
}

func TestSessionManager_Resume_ErrorBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockStore()
	sm := NewSessionManager(store)
	sess, _ := sm.Start(ctx, "/tmp/proj")

	// GetSession error.
	badStore := &getSessionErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if _, err := sm2.Resume(ctx, sess.ID); err == nil {
		t.Error("expected error for GetSession failure")
	}

	// GetMessages error.
	badMsgStore := &getMessagesErrorStore{mockStore: store}
	sm3 := NewSessionManager(badMsgStore)
	if _, err := sm3.Resume(ctx, sess.ID); err == nil {
		t.Error("expected error for GetMessages failure")
	}
}

type getSessionErrorStore struct {
	*mockStore
}

func (m *getSessionErrorStore) GetSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, fmt.Errorf("get session failed")
}

func TestSessionManager_ContinueLast_ErrorBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	if _, err := sm.ContinueLast(ctx, "/tmp/proj"); err == nil {
		t.Fatal("expected error when no sessions exist")
	}

	sess, _ := sm.Start(ctx, "/tmp/proj")
	badStore := &getLastSessionErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if _, err := sm2.ContinueLast(ctx, "/tmp/proj"); err == nil {
		t.Fatal("expected error for GetLastSession failure")
	}

	// GetMessages error after finding last session.
	badMsgStore := &getMessagesErrorStore{mockStore: store}
	sm3 := NewSessionManager(badMsgStore)
	if _, err := sm3.ContinueLast(ctx, "/tmp/proj"); err == nil {
		t.Fatal("expected error for GetMessages failure")
	}

	_ = sess
}

type getLastSessionErrorStore struct {
	*mockStore
}

func (m *getLastSessionErrorStore) GetLastSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, fmt.Errorf("get last session failed")
}

func TestSessionManager_List_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &listSessionsErrorStore{mockStore: newMockStore()}
	sm := NewSessionManager(store)

	if _, err := sm.List(ctx, "/tmp/proj"); err == nil {
		t.Fatal("expected error for ListSessions failure")
	}
}

type listSessionsErrorStore struct {
	*mockStore
}

func (m *listSessionsErrorStore) ListSessions(_ context.Context, _ string, _ int) ([]api.Session, error) {
	return nil, fmt.Errorf("list sessions failed")
}

func TestSessionManager_Get_ErrorBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	if _, err := sm.Get(ctx, "missing"); err == nil {
		t.Fatal("expected error for missing session")
	}

	sess, _ := sm.Start(ctx, "/tmp/proj")
	badStore := &getMessagesErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if _, err := sm2.Get(ctx, sess.ID); err == nil {
		t.Fatal("expected error for GetMessages failure")
	}
}

func TestSessionManager_ClearMessages_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &clearMessagesErrorStore{mockStore: newMockStore()}
	sm := NewSessionManager(store)

	if err := sm.ClearMessages(ctx, "sess"); err == nil {
		t.Fatal("expected error for ClearMessages failure")
	}
}

type clearMessagesErrorStore struct {
	*mockStore
}

func (m *clearMessagesErrorStore) ClearMessages(_ context.Context, _ string) error {
	return fmt.Errorf("clear messages failed")
}

func TestSessionManager_Rename_ErrorBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	if err := sm.Rename(ctx, "missing", "x"); err == nil {
		t.Fatal("expected error for missing session")
	}

	sess, _ := sm.Start(ctx, "/tmp/proj")
	badStore := &updateSessionErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if err := sm2.Rename(ctx, sess.ID, "x"); err == nil {
		t.Fatal("expected error for UpdateSession failure")
	}
}

type updateSessionErrorStore struct {
	*mockStore
}

func (m *updateSessionErrorStore) UpdateSession(_ context.Context, _ *api.Session) error {
	return fmt.Errorf("update session failed")
}

func TestSessionManager_Fork_ErrorBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	if _, err := sm.Fork(ctx, "missing", ""); err == nil {
		t.Fatal("expected error for missing source session")
	}

	sess, _ := sm.Start(ctx, "/tmp/proj")
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hi"})

	badStore := &createSessionErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if _, err := sm2.Fork(ctx, sess.ID, ""); err == nil {
		t.Fatal("expected error for CreateSession failure")
	}

	badUpdateStore := &updateSessionErrorStore{mockStore: store}
	sm3 := NewSessionManager(badUpdateStore)
	if _, err := sm3.Fork(ctx, sess.ID, ""); err == nil {
		t.Fatal("expected error for UpdateSession failure")
	}

	badReplaceStore := &failingReplaceStore{mockStore: store}
	sm4 := NewSessionManager(badReplaceStore)
	if _, err := sm4.Fork(ctx, sess.ID, ""); err == nil {
		t.Fatal("expected error for ReplaceMessages failure")
	}
}

type failingReplaceStore struct {
	*mockStore
}

func (m *failingReplaceStore) ReplaceMessages(_ context.Context, _ string, _ []api.Message) error {
	return fmt.Errorf("replace messages failed")
}

func TestSessionManager_RunHooks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sm := NewSessionManager(newMockStore())

	// Nil runner.
	sm.runHooks(ctx, api.HookSessionStart, "sess")

	// Runner that errors.
	sm.SetHookRunner(&errorHookRunner{})
	sm.runHooks(ctx, api.HookSessionStart, "sess")

	// Cancelled context.
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	sm.SetHookRunner(&recordingHookRunner{})
	sm.runHooks(cancelledCtx, api.HookSessionStart, "sess")
}

func TestSessionManager_Fork_DefaultName_FromID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	forked, err := sm.Fork(ctx, sess.ID, "")
	if err != nil {
		t.Fatalf("fork session: %v", err)
	}

	want := fmt.Sprintf("Fork of %s", sess.ID)
	if forked.Name != want {
		t.Errorf("name = %q, want %q", forked.Name, want)
	}
}

// --- ContextCompressor ---

func TestContextCompressor_SetTokenEstimator_Nil(t *testing.T) {
	t.Parallel()
	c := mustNewContextCompressor(t, nil, 0, 0)
	c.SetTokenEstimator(nil)
	if c.estimator != nil {
		t.Error("nil estimator should be ignored")
	}
}

func TestFindSafeBoundary(t *testing.T) {
	t.Parallel()
	base := time.Now().UTC()
	msgs := []api.Message{
		{ID: "m1", Role: api.RoleUser, CreatedAt: base},
		{ID: "m2", Role: api.RoleAssistant, ToolCalls: []api.ToolCall{{ID: "tc1"}}, CreatedAt: base.Add(time.Minute)},
		{ID: "m3", Role: api.RoleTool, ToolCallID: "tc1", CreatedAt: base.Add(2 * time.Minute)},
	}

	if got := findSafeBoundary(msgs, -1); got != 3 {
		t.Errorf("boundary with keepRecent=-1 = %d, want 3", got)
	}
	if got := findSafeBoundary(msgs, 10); got != 0 {
		t.Errorf("boundary with keepRecent>=len = %d, want 0", got)
	}
	// keepRecent=1 would place boundary at index 2 (tool result); it should
	// walk back to index 1 (assistant), then index 0 (user).
	if got := findSafeBoundary(msgs, 1); got != 0 {
		t.Errorf("boundary = %d, want 0", got)
	}
	// keepRecent=0 means summarize everything.
	if got := findSafeBoundary(msgs, 0); got != 3 {
		t.Errorf("boundary with keepRecent=0 = %d, want 3", got)
	}
}

func TestContextCompressor_Compact_GetMessagesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &getMessagesErrorStore{mockStore: newMockStore()}

	c := mustNewContextCompressor(t, &mockLLMClient{}, 1000, 0)
	_, err := c.Compact(ctx, store, "sess", 2)
	if err == nil {
		t.Fatal("expected error for GetMessages failure")
	}
	if !strings.Contains(err.Error(), "get messages") {
		t.Errorf("error = %q, want get messages", err.Error())
	}
}

func TestContextCompressor_Compact_ReplaceMessagesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &replaceMessagesErrorStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   strings.Repeat("a", 300),
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(_ context.Context, _ []api.Message, _ []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "summary"}, nil
		},
	}
	c := mustNewContextCompressor(t, llm, 500, 0)
	_, err := c.Compact(ctx, store, sess.ID, 2)
	if err == nil {
		t.Fatal("expected error for ReplaceMessages failure")
	}
	if !strings.Contains(err.Error(), "replace messages") {
		t.Errorf("error = %q, want replace messages", err.Error())
	}
}

type replaceMessagesErrorStore struct {
	*mockStore
}

func (m *replaceMessagesErrorStore) ReplaceMessages(_ context.Context, _ string, _ []api.Message) error {
	return fmt.Errorf("replace messages failed")
}

func TestContextCompressor_Compact_SummaryNotSmaller(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Each message is small; with a tiny context window the summary+recent
	// would not be smaller than the originals.
	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   "a", // 1 char ~= 0 tokens
			CreatedAt: time.Now().UTC(),
		})
	}

	called := false
	llm := &mockLLMClient{
		chatFunc: func(_ context.Context, _ []api.Message, _ []api.ToolDefinition) (*api.Message, error) {
			called = true
			return &api.Message{Content: "summary"}, nil
		},
	}
	c := mustNewContextCompressor(t, llm, 10, 0)
	summarized, err := c.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 0 {
		t.Errorf("summarized = %d, want 0", summarized)
	}
	if called {
		t.Error("LLM should not be called when summary is not smaller")
	}
}

// --- Diff ---

func TestComputeFileDiff_EmptyPath(t *testing.T) {
	t.Parallel()
	got, err := ComputeFileDiff("", []byte("x"), "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty diff for empty path, got %q", got)
	}
}

func TestComputeFileDiff_InvalidPath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ComputeFileDiff(filepath.Join(blocked, "secret.txt"), []byte("x"), tmp, []string{blocked})
	if err == nil {
		t.Error("expected error for invalid path")
	}
	if got != "" {
		t.Errorf("expected empty diff for invalid path, got %q", got)
	}
}

func TestComputeFileDiff_NoSandboxRoot(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// With no sandbox root and a relative path, ComputeFileDiff validates the
	// path as-is. Since the file does not exist in the current working
	// directory, the diff treats the old content as empty.
	diff, err := ComputeFileDiff("file.txt", []byte("new"), "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff == "" {
		t.Fatal("expected diff")
	}
	if strings.Contains(diff, "old") {
		t.Errorf("did not expect old content in diff: %s", diff)
	}
	if !strings.Contains(diff, "new") {
		t.Errorf("diff missing expected content: %s", diff)
	}
}

func TestToolCallDiff_UnknownTool(t *testing.T) {
	t.Parallel()
	call := api.ToolCall{ID: "tc1", Name: "read_file", Arguments: `{}`}
	got, err := ToolCallDiff(call, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty diff for unknown tool, got %q", got)
	}
}

func TestToolCallDiff_StrReplaceFile_RelativePath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("alpha beta"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	call := api.ToolCall{
		ID:        "tc1",
		Name:      "str_replace_file",
		Arguments: `{"path":"file.txt","old_string":"alpha","new_string":"gamma"}`,
	}
	diff, err := ToolCallDiff(call, tmp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff == "" {
		t.Fatal("expected diff")
	}
	if !strings.Contains(diff, "alpha") || !strings.Contains(diff, "gamma") {
		t.Errorf("diff missing expected content: %s", diff)
	}
}

func TestToolCallDiff_StrReplaceFile_MissingOldString(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("alpha beta"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	call := api.ToolCall{
		ID:        "tc1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"gamma","new_string":"delta"}`, target),
	}
	diff, err := ToolCallDiff(call, tmp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "" {
		// When the old string is not found, the content is unchanged and the
		// diff is empty.
		t.Errorf("expected empty diff when old string is missing, got %q", diff)
	}
}

// --- Risk ---

func TestRiskRank_Invalid(t *testing.T) {
	t.Parallel()
	if got := riskRank("unknown"); got != 0 {
		t.Errorf("riskRank = %d, want 0", got)
	}
}

func TestRiskEvaluator_ParseArgs_Empty(t *testing.T) {
	t.Parallel()
	e := NewRiskEvaluator(nil, "")
	args, err := e.parseArgs("")
	if err != nil {
		t.Fatalf("parseArgs error: %v", err)
	}
	if args != nil {
		t.Errorf("parseArgs(\"\") = %v, want nil", args)
	}
}

func TestRiskEvaluator_PathMatches(t *testing.T) {
	t.Parallel()
	if !pathMatches("*.go", "main.go") {
		t.Error("expected *.go to match main.go")
	}
	if !pathMatches("main.go", "main.go") {
		t.Error("expected exact match")
	}
	if pathMatches("*.go", "main.txt") {
		t.Error("expected no match")
	}
}

func TestRiskEvaluator_Evaluate_RuleBranches(t *testing.T) {
	t.Parallel()
	sandbox := t.TempDir()

	// Rule tool mismatch.
	e := NewRiskEvaluator([]api.RiskRule{
		{Tool: "shell", Level: api.RiskLevelHigh, Message: "shell"},
	}, sandbox)
	level, _ := e.Evaluate(api.ToolCall{Name: "read_file", Arguments: `{}`})
	if level != api.RiskLevelLow {
		t.Errorf("level = %q, want low", level)
	}

	// Rule path mismatch.
	e = NewRiskEvaluator([]api.RiskRule{
		{Tool: "write_file", Path: "*.md", Level: api.RiskLevelLow, Message: "docs"},
	}, sandbox)
	level, _ = e.Evaluate(api.ToolCall{Name: "write_file", Arguments: `{"path":"main.go"}`})
	if level != api.RiskLevelMedium {
		t.Errorf("level = %q, want medium", level)
	}

	// Rule with invalid level is ignored.
	e = NewRiskEvaluator([]api.RiskRule{
		{Tool: "read_file", Level: "invalid", Message: "ignored"},
	}, sandbox)
	level, _ = e.Evaluate(api.ToolCall{Name: "read_file", Arguments: `{}`})
	if level != api.RiskLevelLow {
		t.Errorf("level = %q, want low", level)
	}

	// Rule without message uses generated reason.
	e = NewRiskEvaluator([]api.RiskRule{
		{Tool: "shell", Level: api.RiskLevelMedium},
	}, sandbox)
	level, reason := e.Evaluate(api.ToolCall{Name: "shell", Arguments: `{}`})
	if level != api.RiskLevelMedium {
		t.Errorf("level = %q, want medium", level)
	}
	if !strings.Contains(reason, "rule") {
		t.Errorf("reason = %q, want containing rule", reason)
	}
}

func TestRiskEvaluator_PathEscapes_EmptyRoot(t *testing.T) {
	t.Parallel()
	e := NewRiskEvaluator(nil, "")
	if e.pathEscapes("/tmp/safe.txt") {
		t.Error("expected no escape for non-sensitive path when sandbox root is empty")
	}
	// Sensitive paths are still treated as escapes even without a sandbox root.
	if !e.pathEscapes("/etc/passwd") {
		t.Error("expected escape for sensitive path")
	}
}

// --- Tokens ---

func TestHeuristicTokenEstimator_ToolCallID(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	msgs := []api.Message{{
		Role:       api.RoleTool,
		Content:    "result",
		ToolCallID: "call_abc123",
	}}
	got := e.Estimate(msgs)
	// 3 overhead + 1 for content + 5 for tool call ID overhead + 2 for ID.
	want := 3 + 1 + 10/2 + len("call_abc123")/4
	if got != want {
		t.Errorf("estimate = %d, want %d", got, want)
	}
}

func TestHeuristicTokenEstimator_InvalidUTF8(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	got := e.estimateString("\xff\xfe")
	if got <= 0 {
		t.Errorf("estimate = %d, want > 0", got)
	}
}

// --- Additional Group B coverage tests ---

func TestTurnManager_StartTurn_DrainsStaleApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t,
		&mockLLMClient{chatStreamFunc: streamChunks(api.StreamChunk{Done: true})},
		&mockToolExecutor{},
		&mockApprovalGate{},
		store,
		nil,
	)

	// Pre-fill the approval channel with a stale payload from a previous turn.
	tm.approvalCh <- approvalPayload{requestID: 42, decisions: map[string]api.ApprovalDecision{}}

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}

func TestTurnManager_StartTurn_MaxHistoryConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 10; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("msg%d", i),
			CreatedAt: time.Now().UTC(),
		})
	}

	cfg := &mockConfigProvider{cfg: &api.Config{Session: api.SessionConfig{MaxHistory: 3}}}
	tm := newTestTurnManager(t,
		&mockLLMClient{chatStreamFunc: streamChunks(api.StreamChunk{Done: true})},
		&mockToolExecutor{},
		&mockApprovalGate{},
		store,
		cfg,
	)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}

type failingSaveTurnOnToolCallsStore struct {
	*mockStore
}

func (m *failingSaveTurnOnToolCallsStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	if turn.State == api.TurnToolCalls {
		return fmt.Errorf("injected save turn failure")
	}
	return m.mockStore.SaveTurn(ctx, sessionID, turn)
}

func TestTurnManager_Run_SaveTurnAfterToolCallsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingSaveTurnOnToolCallsStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}

func TestTurnManager_Run_StaleApprovalPayload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "write_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{{Name: "write_file", Description: "write"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, false }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	approvalSeen := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			if e.Type == api.TurnEventApprovalRequest {
				close(approvalSeen)
			}
		}
		close(done)
	}()

	select {
	case <-approvalSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	// Inject a stale payload directly; the loop should treat all pending calls as denied.
	tm.approvalCh <- approvalPayload{requestID: 999, decisions: map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
	if len(turn.Results) != 1 || turn.Results[0].Error != "tool call denied (stale approval)" {
		t.Errorf("expected stale denial result, got %+v", turn.Results)
	}
}

func TestTurnManager_Run_MissingApprovalDecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "write_file", Arguments: `{}`},
						{ID: "tc2", Name: "write_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{{Name: "write_file", Description: "write"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, false }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	approvalSeen := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			if e.Type == api.TurnEventApprovalRequest {
				close(approvalSeen)
			}
		}
		close(done)
	}()

	select {
	case <-approvalSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	// Approve only tc1; tc2 is missing from the map and should be denied.
	_, reqID := tm.PendingApprovals()
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err != nil {
		t.Fatalf("resume with approval: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
	if len(turn.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(turn.Results))
	}
	if turn.Results[0].Error != "" {
		t.Errorf("tc1 unexpected error: %s", turn.Results[0].Error)
	}
	if turn.Results[1].Error != "tool call denied" {
		t.Errorf("tc2 expected denial, got: %s", turn.Results[1].Error)
	}
}

type roleFailingAppendStore struct {
	*mockStore
	role api.Role
}

func (m *roleFailingAppendStore) AppendMessage(_ context.Context, sessionID string, msg api.Message) error {
	if msg.Role == m.role {
		return fmt.Errorf("injected %s append failure", m.role)
	}
	return m.mockStore.AppendMessage(context.Background(), sessionID, msg)
}

func TestTurnManager_Run_AssistantAppendError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &roleFailingAppendStore{mockStore: newMockStore(), role: api.RoleAssistant}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
				{ID: "tc1", Name: "read_file", Arguments: `{}`},
			}},
		),
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "append message") {
		t.Errorf("error = %q, want append message", turn.Error)
	}
}

func TestTurnManager_Run_ToolResultErrorOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
				{ID: "tc1", Name: "read_file", Arguments: `{}`},
			}},
		),
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Error: "boom"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	var toolMsg *api.Message
	for i := range msgs {
		if msgs[i].Role == api.RoleTool {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected tool message")
	}
	want := "Error: boom"
	if toolMsg.Content != want {
		t.Errorf("tool content = %q, want %q", toolMsg.Content, want)
	}
}

func TestTurnManager_Run_SecondStreamError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return nil, fmt.Errorf("second stream failed")
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "chat stream") {
		t.Errorf("error = %q, want chat stream", turn.Error)
	}
}

type failingSaveTurnOnIdleStore struct {
	*mockStore
}

func (m *failingSaveTurnOnIdleStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	if turn.State == api.TurnIdle {
		return fmt.Errorf("injected idle save turn failure")
	}
	return m.mockStore.SaveTurn(ctx, sessionID, turn)
}

func TestTurnManager_Run_FinalSaveTurnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingSaveTurnOnIdleStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "final"},
			api.StreamChunk{Done: true},
		),
	}
	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}

func TestTurnManager_ConsumeStream_ErrorChunk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	streamCh := make(chan api.StreamChunk, 1)
	streamCh <- api.StreamChunk{Error: fmt.Errorf("chunk error")}
	close(streamCh)

	eventCh := make(chan api.TurnEvent, 4)
	_, _, err := tm.consumeStream(ctx, "", &api.Turn{}, streamCh, eventCh)
	if err == nil || !strings.Contains(err.Error(), "chunk error") {
		t.Fatalf("expected chunk error, got %v", err)
	}
}

func TestTurnManager_ConsumeStream_MaxSizeCtxCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	streamCh := make(chan api.StreamChunk, 1)
	streamCh <- api.StreamChunk{Content: strings.Repeat("a", maxStreamResponseSize+1)}
	close(streamCh)

	// Unbuffered event channel ensures the max-size error send blocks until ctx is canceled.
	eventCh := make(chan api.TurnEvent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tm.consumeStream(ctx, "", &api.Turn{}, streamCh, eventCh)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for consumeStream to finish")
	}
}

func TestTurnManager_ConsumeStream_ContentCtxCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	tm := newTestTurnManager(t, nil, nil, nil, nil, nil)

	streamCh := make(chan api.StreamChunk, 1)
	streamCh <- api.StreamChunk{Content: "hello"}
	close(streamCh)

	// Unbuffered channel blocks the content-event send until ctx is canceled.
	eventCh := make(chan api.TurnEvent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tm.consumeStream(ctx, "", &api.Turn{}, streamCh, eventCh)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for consumeStream to finish")
	}
}

func TestTurnManager_PersistPartialResponse_AppendError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &roleFailingAppendStore{mockStore: newMockStore(), role: api.RoleAssistant}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, nil, nil, nil, store, nil)
	turn := &api.Turn{ID: "t1"}
	tm.persistPartialResponse(ctx, sess.ID, turn, "partial content")

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 0 {
		t.Errorf("expected no messages after append failure, got %d", len(msgs))
	}
}

func TestSessionManager_Fork_GetMessagesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)
	sess, _ := sm.Start(ctx, "/tmp/proj")

	badStore := &getMessagesErrorStore{mockStore: store}
	sm2 := NewSessionManager(badStore)
	if _, err := sm2.Fork(ctx, sess.ID, "fork"); err == nil {
		t.Fatal("expected error for GetMessages failure")
	}
}

func TestComputeFileDiff_RelativePathWithSandboxRoot(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	diff, err := ComputeFileDiff("file.txt", []byte("new"), tmp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff == "" {
		t.Fatal("expected diff")
	}
	if !strings.Contains(diff, "old") || !strings.Contains(diff, "new") {
		t.Errorf("diff missing expected content: %s", diff)
	}
}

func TestContextCompressor_Compact_NegativeKeepRecent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 3; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   strings.Repeat("a", 300),
			CreatedAt: time.Now().UTC(),
		})
	}

	c := mustNewContextCompressor(t, &mockLLMClient{chatFunc: func(context.Context, []api.Message, []api.ToolDefinition) (*api.Message, error) {
		t.Fatal("LLM should not be called")
		return nil, nil
	}}, 10000, 0)

	summarized, err := c.Compact(ctx, store, sess.ID, -1)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 0 {
		t.Errorf("summarized = %d, want 0", summarized)
	}
}

func TestContextCompressor_Compact_EmptyMiddle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	base := time.Now().UTC()
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "s1", Role: api.RoleSystem, Content: strings.Repeat("a", 200), CreatedAt: base})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: strings.Repeat("b", 200), CreatedAt: base.Add(time.Minute)})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "plan", ToolCalls: []api.ToolCall{{ID: "tc1"}}, CreatedAt: base.Add(2 * time.Minute)})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m3", Role: api.RoleTool, Content: "result", ToolCallID: "tc1", CreatedAt: base.Add(3 * time.Minute)})

	c := mustNewContextCompressor(t, &mockLLMClient{chatFunc: func(context.Context, []api.Message, []api.ToolDefinition) (*api.Message, error) {
		t.Fatal("LLM should not be called")
		return nil, nil
	}}, 100, 0)

	summarized, err := c.Compact(ctx, store, sess.ID, 1)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 0 {
		t.Errorf("summarized = %d, want 0", summarized)
	}
}

func TestContextCompressor_Compact_InputTruncation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	base := time.Now().UTC()
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "s1", Role: api.RoleSystem, Content: strings.Repeat("a", 50), CreatedAt: base})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: strings.Repeat("b", 5000), CreatedAt: base.Add(time.Minute)})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: strings.Repeat("c", 5000), CreatedAt: base.Add(2 * time.Minute)})

	var prompt string
	called := false
	llm := &mockLLMClient{chatFunc: func(_ context.Context, msgs []api.Message, _ []api.ToolDefinition) (*api.Message, error) {
		called = true
		if len(msgs) != 1 {
			t.Errorf("expected 1 summarization message, got %d", len(msgs))
		}
		prompt = msgs[0].Content
		return &api.Message{Content: "summary"}, nil
	}}

	c := mustNewContextCompressor(t, llm, 2000, time.Second)
	summarized, err := c.Compact(ctx, store, sess.ID, 0)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Only the newest middle message fits in the summarization budget, so the
	// returned count must reflect what was actually sent to the LLM, not the
	// original size of middle.
	if summarized != 1 {
		t.Errorf("summarized = %d, want 1", summarized)
	}
	if !called {
		t.Error("expected LLM to be called")
	}
	if strings.Contains(prompt, strings.Repeat("b", 5000)) {
		t.Error("prompt contains dropped message m1")
	}
	if !strings.Contains(prompt, strings.Repeat("c", 5000)) {
		t.Error("prompt missing included message m2")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 2 { // leading system + summary
		t.Errorf("expected 2 messages after compact, got %d", len(msgs))
	}
}

func TestRiskEvaluator_PathEscapes_RootEquals(t *testing.T) {
	t.Parallel()
	sandbox := t.TempDir()
	e := NewRiskEvaluator(nil, sandbox)

	level, reason := e.Evaluate(api.ToolCall{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%s"}`, sandbox)})
	if level != api.RiskLevelLow {
		t.Errorf("level = %q, want low", level)
	}
	if strings.Contains(reason, "escapes") {
		t.Errorf("reason should not mention escape: %s", reason)
	}
}

// --- Extra Group B edge-case coverage ---

func TestTurnManager_Run_ToolResultOutputAndError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
				{ID: "tc1", Name: "read_file", Arguments: `{}`},
			}},
		),
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "partial output", Error: "boom"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	var toolMsg *api.Message
	for i := range msgs {
		if msgs[i].Role == api.RoleTool {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected tool message")
	}
	if !strings.Contains(toolMsg.Content, "partial output") || !strings.Contains(toolMsg.Content, "Error: boom") {
		t.Errorf("tool content = %q, want combined output and error", toolMsg.Content)
	}
}

func TestTurnManager_Run_FinalAssistantAppendError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &failingAppendStore{mockStore: newMockStore(), failAfter: 1}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "final"},
			api.StreamChunk{Done: true},
		),
	}
	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}

func TestTurnManager_Run_SecondConsumeStreamError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Content: "partial"},
				api.StreamChunk{Error: fmt.Errorf("second stream broken")},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "data"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalYes, true }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn)
	}
	if !strings.Contains(turn.Error, "second stream broken") {
		t.Errorf("error = %q, want second stream broken", turn.Error)
	}
}

func TestTurnManager_Run_DiffUnknownCallID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "write_file", Arguments: `{"path":"/tmp/out.txt","content":"x"}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(api.StreamChunk{Done: true})(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(_ context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{{Name: "write_file", Description: "write"}},
	}
	approval := &mockApprovalGate{shouldAutoApprove: func(_ api.ToolCall) (api.ApprovalDecision, bool) { return api.ApprovalNo, false }}
	tm := newTestTurnManager(t, llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	approvalSeen := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			if e.Type == api.TurnEventApprovalRequest {
				close(approvalSeen)
			}
		}
		close(done)
	}()

	select {
	case <-approvalSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	_, reqID := tm.PendingApprovals()
	// Request diffs for both the known call and an unknown call; the unknown one
	// should be ignored without breaking the known diff.
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{
		"tc1":     api.ApprovalDiff,
		"unknown": api.ApprovalDiff,
	}); err != nil {
		t.Fatalf("resume with diff: %v", err)
	}

	// Approve the known call so the turn can complete.
	_, reqID = tm.PendingApprovals()
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err != nil {
		t.Fatalf("resume with approval: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected idle turn, got %v", turn)
	}
}
