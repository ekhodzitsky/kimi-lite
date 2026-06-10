package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func streamChunks(chunks ...api.StreamChunk) func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
		ch := make(chan api.StreamChunk)
		go func() {
			defer close(ch)
			for _, c := range chunks {
				select {
				case ch <- c:
				case <-ctx.Done():
					return
				}
			}
		}()
		return ch, nil
	}
}

func TestTurnManager_RunTurn_Simple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "Hello"},
			api.StreamChunk{Content: " world"},
			api.StreamChunk{Done: true},
		),
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{}, nil
		},
		defs: []api.ToolDefinition{},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	for c := range outCh {
		contents = append(contents, c)
	}

	if len(contents) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(contents))
	}
	if contents[0] != "Hello" {
		t.Errorf("chunk[0] = %q, want %q", contents[0], "Hello")
	}
	if contents[1] != " world" {
		t.Errorf("chunk[1] = %q, want %q", contents[1], " world")
	}

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", turn.State)
	}
	if turn.Response != "Hello world" {
		t.Errorf("response = %q, want %q", turn.Response, "Hello world")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != api.RoleUser {
		t.Errorf("msg[0].role = %q, want user", msgs[0].Role)
	}
	if msgs[1].Role != api.RoleAssistant {
		t.Errorf("msg[1].role = %q, want assistant", msgs[1].Role)
	}
}

func TestTurnManager_RunTurn_WithToolCalls(t *testing.T) {
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
					api.StreamChunk{Content: "Let me check"},
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "read_file", Arguments: `{"path":"/tmp/test.txt"}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Content: "The file says hello"},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "file content"}, nil
		},
		defs: []api.ToolDefinition{
			{Name: "read_file", Description: "read"},
		},
		readOnlyTools: map[string]bool{"read_file": true},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Read the file")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	for c := range outCh {
		contents = append(contents, c)
	}

	if len(contents) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %v", len(contents), contents)
	}
	if contents[0] != "Let me check" {
		t.Errorf("chunk[0] = %q, want %q", contents[0], "Let me check")
	}
	if contents[1] != "The file says hello" {
		t.Errorf("chunk[1] = %q, want %q", contents[1], "The file says hello")
	}

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", turn.State)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(turn.ToolCalls))
	}
	if len(turn.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(turn.Results))
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	// user + assistant(with tool calls) + tool + assistant(final)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
}

func TestTurnManager_RunTurn_ManualApproval(t *testing.T) {
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
					api.StreamChunk{Content: "Let me write"},
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "write_file", Arguments: `{"path":"/tmp/out.txt","content":"x"}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Content: "Done writing"},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs:          []api.ToolDefinition{{Name: "write_file", Description: "write"}},
		readOnlyTools: map[string]bool{},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalNo, false // manual approval required
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Write a file")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// Read chunks in background since outCh won't close until resume
	var contents []string
	done := make(chan struct{})
	go func() {
		for c := range outCh {
			contents = append(contents, c)
		}
		close(done)
	}()

	// Wait for pending approval state
	var turn *api.Turn
	for i := 0; i < 100; i++ {
		turn = tm.CurrentTurn()
		if turn != nil && turn.State == api.TurnWaitingApproval {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", turn.State)
	}

	// Approve and resume
	if err := tm.ResumeWithApproval(ctx, sess.ID, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err != nil {
		t.Fatalf("resume with approval: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	turn = tm.CurrentTurn()
	if turn.State != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", turn.State)
	}
	if len(turn.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(turn.Results))
	}
	if turn.Results[0].Error != "" {
		t.Errorf("unexpected result error: %s", turn.Results[0].Error)
	}
}

func TestTurnManager_RunTurn_MaxTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")
	_ = store.SaveTurn(ctx, sess.ID, api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()})
	_ = store.SaveTurn(ctx, sess.ID, api.Turn{ID: "t2", State: api.TurnIdle, StartedAt: time.Now().UTC()})

	llm := &mockLLMClient{}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 2}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	_, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err == nil {
		t.Fatal("expected error for max turns reached")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("error = %q, want max turns error", err.Error())
	}
}

func TestTurnManager_RunTurn_StreamError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				ch <- api.StreamChunk{Content: "Partial"}
				ch <- api.StreamChunk{Error: fmt.Errorf("stream broken")}
			}()
			return ch, nil
		},
	}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
		return api.ApprovalYes, true
	}}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	for c := range outCh {
		contents = append(contents, c)
	}

	if len(contents) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(contents))
	}
	if contents[0] != "Partial" {
		t.Errorf("contents[0] = %q, want Partial", contents[0])
	}
	if !strings.Contains(contents[1], "stream broken") {
		t.Errorf("contents[1] = %q, want error with stream broken", contents[1])
	}

	turn := tm.CurrentTurn()
	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
	if !strings.Contains(turn.Error, "stream broken") {
		t.Errorf("error = %q, want stream broken", turn.Error)
	}
}

func TestTurnManager_RunTurn_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				select {
				case ch <- api.StreamChunk{Content: "Hello"}:
				case <-ctx.Done():
					return
				}
				select {
				case <-time.After(100 * time.Millisecond):
					ch <- api.StreamChunk{Content: " world"}
				case <-ctx.Done():
					return
				}
				ch <- api.StreamChunk{Done: true}
			}()
			return ch, nil
		},
	}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
		return api.ApprovalYes, true
	}}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	for c := range outCh {
		contents = append(contents, c)
		if len(contents) == 1 {
			cancel() // Cancel after first chunk
		}
	}

	// Should have received at most 1 chunk before cancellation.
	if len(contents) == 0 {
		t.Error("expected at least one chunk before cancellation")
	}

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
}

func TestTurnManager_RunTurn_LLMError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return nil, fmt.Errorf("llm unavailable")
		},
	}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	_, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err == nil {
		t.Fatal("expected error for LLM failure")
	}
	if !strings.Contains(err.Error(), "llm unavailable") {
		t.Errorf("error = %q, want llm unavailable", err.Error())
	}
}

func TestTurnManager_CurrentTurn_Nil(t *testing.T) {
	t.Parallel()
	tm := NewTurnManager(nil, nil, nil, nil, nil)
	if tm.CurrentTurn() != nil {
		t.Error("expected nil current turn")
	}
}

func TestTurnManager_RunTurn_NoConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "OK"},
			api.StreamChunk{Done: true},
		),
	}
	tools := &mockToolExecutor{defs: []api.ToolDefinition{}}
	approval := &mockApprovalGate{shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
		return api.ApprovalYes, true
	}}

	tm := NewTurnManager(llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	for c := range outCh {
		contents = append(contents, c)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(contents))
	}
}

func TestTurnManager_ToolCallID_PreservedAcrossRounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	var secondRoundMessages []api.Message
	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			callCount++
			if callCount == 2 {
				secondRoundMessages = append([]api.Message(nil), messages...)
			}
			if callCount == 1 {
				return streamChunks(
					api.StreamChunk{Content: "Let me check"},
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "call-abc", Name: "read_file", Arguments: `{"path":"/tmp/test.txt"}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Content: "Done"},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "file content"}, nil
		},
		defs:          []api.ToolDefinition{{Name: "read_file", Description: "read"}},
		readOnlyTools: map[string]bool{"read_file": true},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Read the file")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	// Find the tool message.
	var toolMsg *api.Message
	for i := range msgs {
		if msgs[i].Role == api.RoleTool {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected a tool message in store")
	}
	if toolMsg.ToolCallID != "call-abc" {
		t.Errorf("tool message ToolCallID = %q, want call-abc", toolMsg.ToolCallID)
	}

	// The second ChatStream must receive the tool message with the matching tool_call_id.
	if len(secondRoundMessages) == 0 {
		t.Fatal("expected second round messages to be captured")
	}
	var found bool
	for _, m := range secondRoundMessages {
		if m.Role == api.RoleTool && m.ToolCallID == "call-abc" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("second round ChatStream did not receive tool message with ToolCallID=call-abc")
	}
}

func TestTurnManager_RunTurn_Overlapping(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "hello"},
			api.StreamChunk{Done: true},
		),
	}
	tm := NewTurnManager(llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	ctx := context.Background()
	ch1, err := tm.RunTurn(ctx, "sess-1", "first")
	if err != nil {
		t.Fatalf("first RunTurn: %v", err)
	}

	// Second RunTurn while first is still active should fail.
	_, err = tm.RunTurn(ctx, "sess-1", "second")
	if err == nil {
		t.Fatal("expected error for overlapping RunTurn, got nil")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error = %q, want containing 'already in progress'", err.Error())
	}

	// Consume first stream to completion.
	for range ch1 {
	}

	// After completion, a new RunTurn should succeed.
	ch2, err := tm.RunTurn(ctx, "sess-1", "third")
	if err != nil {
		t.Fatalf("RunTurn after completion: %v", err)
	}
	for range ch2 {
	}
}
