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
	for e := range outCh {
		if e.Type == api.TurnEventContent {
			contents = append(contents, e.Content)
		}
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
	for e := range outCh {
		if e.Type == api.TurnEventContent {
			contents = append(contents, e.Content)
		}
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

func TestTurnManager_RunTurn_ShellNonZeroExitPreservesOutput(t *testing.T) {
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
					api.StreamChunk{Content: "Let me run a command"},
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc1", Name: "shell", Arguments: `{"command":"echo hi; exit 1"}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Content: "Done"},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 10}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Run a command")
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
		t.Fatal("expected a tool message in store")
	}
	if !strings.Contains(toolMsg.Content, "hi") {
		t.Errorf("tool message content = %q, want containing %q", toolMsg.Content, "hi")
	}
	if !strings.Contains(toolMsg.Content, "[exit status 1]") {
		t.Errorf("tool message content = %q, want containing %q", toolMsg.Content, "[exit status 1]")
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
		defs: []api.ToolDefinition{{Name: "write_file", Description: "write"}},
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
	approvalSeen := make(chan struct{})
	go func() {
		for e := range outCh {
			switch e.Type {
			case api.TurnEventContent:
				contents = append(contents, e.Content)
			case api.TurnEventApprovalRequest:
				close(approvalSeen)
			}
		}
		close(done)
	}()

	// Wait deterministically for the approval request event.
	select {
	case <-approvalSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for approval request event")
	}

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnWaitingApproval {
		t.Errorf("state = %d, want TurnWaitingApproval", turn.State)
	}

	// Approve and resume
	_, reqID := tm.PendingApprovals()
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err != nil {
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

func TestTurnManager_RunTurn_MaxTurnsAbove100(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Seed 101 completed turns.
	for i := 0; i < 101; i++ {
		_ = store.SaveTurn(ctx, sess.ID, api.Turn{
			ID:        fmt.Sprintf("t%d", i),
			State:     api.TurnIdle,
			StartedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{}
	tools := &mockToolExecutor{}
	approval := &mockApprovalGate{}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 100}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	_, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err == nil {
		t.Fatal("expected error for max turns reached")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("error = %q, want max turns error", err.Error())
	}
}

func TestTurnManager_RunTurn_MaxTurnsIgnoresErrored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	_ = store.SaveTurn(ctx, sess.ID, api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()})
	_ = store.SaveTurn(ctx, sess.ID, api.Turn{ID: "t2", State: api.TurnIdle, StartedAt: time.Now().UTC()})
	_ = store.SaveTurn(ctx, sess.ID, api.Turn{ID: "t3", State: api.TurnError, StartedAt: time.Now().UTC()})

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
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 3}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected turn to complete, got state %v", turn)
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
	var streamErr error
	for e := range outCh {
		if e.Type == api.TurnEventContent {
			contents = append(contents, e.Content)
		}
		if e.Type == api.TurnEventError {
			streamErr = e.Error
		}
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 content chunk, got %d", len(contents))
	}
	if contents[0] != "Partial" {
		t.Errorf("contents[0] = %q, want Partial", contents[0])
	}
	if streamErr == nil || !strings.Contains(streamErr.Error(), "stream broken") {
		t.Errorf("streamErr = %v, want error with stream broken", streamErr)
	}

	turn := tm.CurrentTurn()
	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
	if !strings.Contains(turn.Error, "stream broken") {
		t.Errorf("error = %q, want stream broken", turn.Error)
	}
	if turn.Response != "Partial" {
		t.Errorf("response = %q, want Partial", turn.Response)
	}
	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	var foundPartialAssistant bool
	for _, m := range msgs {
		if m.Role == api.RoleAssistant && m.Content == "Partial" {
			foundPartialAssistant = true
			break
		}
	}
	if !foundPartialAssistant {
		t.Errorf("expected partial assistant message appended, got messages: %+v", msgs)
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
	for e := range outCh {
		if e.Type == api.TurnEventContent {
			contents = append(contents, e.Content)
		}
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

func TestTurnManager_CurrentTurn_DeepCopy(t *testing.T) {
	t.Parallel()
	tm := NewTurnManager(nil, nil, nil, nil, nil)
	tm.turn = &api.Turn{
		ID:    "turn-1",
		State: api.TurnThinking,
		ToolCalls: []api.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: `{}`},
		},
		Results: []api.ToolResult{
			{CallID: "tc1", Name: "read_file", Output: "hello"},
		},
	}

	copy1 := tm.CurrentTurn()
	copy2 := tm.CurrentTurn()

	// Modifying copy1 should not affect copy2 or the original.
	copy1.ToolCalls[0].Name = "modified"
	copy1.Results[0].Output = "modified"

	if copy2.ToolCalls[0].Name != "read_file" {
		t.Errorf("copy2.ToolCalls[0].Name = %q, want read_file", copy2.ToolCalls[0].Name)
	}
	if copy2.Results[0].Output != "hello" {
		t.Errorf("copy2.Results[0].Output = %q, want hello", copy2.Results[0].Output)
	}
	if tm.turn.ToolCalls[0].Name != "read_file" {
		t.Errorf("original.ToolCalls[0].Name = %q, want read_file", tm.turn.ToolCalls[0].Name)
	}
	if tm.turn.Results[0].Output != "hello" {
		t.Errorf("original.Results[0].Output = %q, want hello", tm.turn.Results[0].Output)
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
	for e := range outCh {
		if e.Type == api.TurnEventContent {
			contents = append(contents, e.Content)
		}
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
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
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

func TestTurnManager_RunTurn_StaleRequestIDIgnored(t *testing.T) {
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
						{ID: "tc1", Name: "shell", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			if callCount == 2 {
				return streamChunks(
					api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
						{ID: "tc2", Name: "shell", Arguments: `{}`},
					}},
				)(ctx, messages, tools)
			}
			return streamChunks(
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{{Name: "shell", Description: "shell"}},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalNo, false // manual approval required
		},
	}

	tm := NewTurnManager(llm, tools, approval, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "test input")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var contents []string
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			if e.Type == api.TurnEventContent {
				contents = append(contents, e.Content)
			}
		}
		close(done)
	}()

	// Wait for pending approval state.
	for i := 0; i < 100; i++ {
		turn := tm.CurrentTurn()
		if turn != nil && turn.State == api.TurnWaitingApproval {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Get the current requestID.
	_, reqID := tm.PendingApprovals()

	// Send a stale approval (wrong requestID) — should be rejected.
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID-1, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err == nil {
		t.Fatal("expected error for stale requestID")
	}

	// Send a matching approval.
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID, map[string]api.ApprovalDecision{"tc1": api.ApprovalYes}); err != nil {
		t.Fatalf("resume with approval: %v", err)
	}

	// Second round also needs approval.
	var reqID2 int64
	for i := 0; i < 100; i++ {
		calls, id := tm.PendingApprovals()
		if len(calls) > 0 && id > reqID {
			reqID2 = id
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reqID2 == 0 {
		t.Fatal("expected second round pending approvals")
	}
	if err := tm.ResumeWithApproval(ctx, sess.ID, reqID2, map[string]api.ApprovalDecision{"tc2": api.ApprovalYes}); err != nil {
		t.Fatalf("resume with approval round 2: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	turn := tm.CurrentTurn()
	if turn.State != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", turn.State)
	}
	if len(turn.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(turn.Results))
	}
	if turn.Results[0].Error != "" {
		t.Errorf("unexpected result 0 error: %s", turn.Results[0].Error)
	}
	if turn.Results[1].Error != "" {
		t.Errorf("unexpected result 1 error: %s", turn.Results[1].Error)
	}
}

func TestTurnManager_executeToolCalls_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	executeCount := 0
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			executeCount++
			if call.ID == "tc1" {
				cancel() // Cancel context after first call
			}
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{
			{Name: "read_file", Description: "read"},
		},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}

	tm := NewTurnManager(&mockLLMClient{}, tools, approval, store, nil)
	turn := &api.Turn{ID: "turn-1", State: api.TurnToolCalls}

	calls := []api.ToolCall{
		{ID: "tc1", Name: "read_file", Arguments: `{}`},
		{ID: "tc2", Name: "read_file", Arguments: `{}`},
		{ID: "tc3", Name: "read_file", Arguments: `{}`},
	}

	results, pending := tm.executeToolCalls(ctx, sess.ID, turn, calls)

	if len(pending) != 0 {
		t.Fatalf("expected no pending calls, got %d", len(pending))
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if executeCount != 1 {
		t.Fatalf("expected 1 Execute call before cancellation, got %d", executeCount)
	}
	if results[0].Error != "" {
		t.Errorf("result[0] error = %q, want empty", results[0].Error)
	}
	if results[1].Error == "" || !strings.Contains(results[1].Error, "context cancelled") {
		t.Errorf("result[1] error = %q, want context cancelled", results[1].Error)
	}
	if results[2].Error == "" || !strings.Contains(results[2].Error, "context cancelled") {
		t.Errorf("result[2] error = %q, want context cancelled", results[2].Error)
	}
}

func TestTurnManager_RunTurn_StreamErrorTypedEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	streamErr := fmt.Errorf("llm stream exploded")
	llm := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "partial"},
			api.StreamChunk{Error: streamErr},
		),
	}
	tm := NewTurnManager(llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "test")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var gotContent bool
	var gotError *api.TurnEvent
	for ev := range outCh {
		switch ev.Type {
		case api.TurnEventContent:
			gotContent = true
		case api.TurnEventError:
			gotError = &ev
		case api.TurnEventDone:
			t.Fatal("expected no Done event after error")
		}
	}

	if !gotContent {
		t.Error("expected content event before error")
	}
	if gotError == nil {
		t.Fatal("expected typed error event")
	}
	if gotError.Error != streamErr {
		t.Errorf("error = %v, want %v", gotError.Error, streamErr)
	}
	// Verify the turn state reflects the error.
	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnError {
		t.Fatalf("expected turn state TurnError, got %v", turn)
	}
}

// failingAppendStore wraps a mockStore and fails AppendMessage after a set
// number of successful calls.
type failingAppendStore struct {
	*mockStore
	failAfter int
}

func (m *failingAppendStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if m.failAfter == 0 {
		return fmt.Errorf("injected append failure")
	}
	m.failAfter--
	return m.mockStore.AppendMessage(ctx, sessionID, msg)
}

func TestTurnManager_RunTurn_AppendMessageFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	baseStore := newMockStore()
	store := &failingAppendStore{mockStore: baseStore, failAfter: 2}
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
				api.StreamChunk{Content: "Done"},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "file content"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
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

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnError {
		t.Errorf("state = %d, want TurnError", turn.State)
	}
	if turn.Error == "" || !strings.Contains(turn.Error, "append message") {
		t.Errorf("error = %q, want containing 'append message'", turn.Error)
	}
}

func TestTurnManager_RunTurn_EmptyTrailingAssistantSkipped(t *testing.T) {
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
			// Final round returns empty/whitespace content.
			return streamChunks(
				api.StreamChunk{Content: "   "},
				api.StreamChunk{Done: true},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "file content"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
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
	// user + assistant(with tool calls) + tool + no empty trailing assistant
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[2].Role != api.RoleTool {
		t.Errorf("msg[2].role = %q, want tool", msgs[2].Role)
	}
}

func TestTurnManager_RunTurn_MaxToolRounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return streamChunks(
				api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
					{ID: "tc1", Name: "read_file", Arguments: `{"path":"/tmp/test.txt"}`},
				}},
			)(ctx, messages, tools)
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Name: call.Name, Output: "done"}, nil
		},
		defs: []api.ToolDefinition{{Name: "read_file", Description: "read"}},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}
	cfg := &mockConfigProvider{cfg: &api.Config{Behavior: api.BehaviorConfig{MaxTurns: 50, MaxToolRounds: 3}}}

	tm := NewTurnManager(llm, tools, approval, store, cfg)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Read the file")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	for range outCh {
	}

	turn := tm.CurrentTurn()
	if turn == nil {
		t.Fatal("expected current turn")
	}
	if turn.State != api.TurnError {
		t.Fatalf("expected TurnError, got %v", turn.State)
	}
	if !strings.Contains(turn.Error, "max tool rounds (3) exceeded") {
		t.Errorf("error = %q, want max tool rounds (3) exceeded", turn.Error)
	}
}

// spyStore wraps a mockStore and counts SaveTurn calls while recording
// every turn that is persisted.
type spyStore struct {
	*mockStore
	saveTurnCalls int
	savedTurns    []api.Turn
}

func (s *spyStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	s.saveTurnCalls++
	s.savedTurns = append(s.savedTurns, turn)
	return s.mockStore.SaveTurn(ctx, sessionID, turn)
}

// saveCaptureLLM wraps a mockLLMClient and records how many SaveTurn calls
// had occurred when ChatStream was first invoked.
type saveCaptureLLM struct {
	*mockLLMClient
	spy                   *spyStore
	saveTurnsAtChatStream int
}

func (c *saveCaptureLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	c.saveTurnsAtChatStream = c.spy.saveTurnCalls
	return c.mockLLMClient.ChatStream(ctx, messages, tools)
}

func TestTurnManager_RunTurn_SingleSaveBeforeStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &spyStore{mockStore: newMockStore()}
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	baseLLM := &mockLLMClient{
		chatStreamFunc: streamChunks(
			api.StreamChunk{Content: "Hello"},
			api.StreamChunk{Done: true},
		),
	}
	llm := &saveCaptureLLM{mockLLMClient: baseLLM, spy: store}

	tools := &mockToolExecutor{defs: []api.ToolDefinition{}}
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

	for range outCh {
	}

	// Exactly one SaveTurn must occur before the first ChatStream call.
	if llm.saveTurnsAtChatStream != 1 {
		t.Errorf("SaveTurn calls before first ChatStream = %d, want 1", llm.saveTurnsAtChatStream)
	}

	// The pre-stream persisted state must be TurnStreaming.
	if len(store.savedTurns) < 1 {
		t.Fatal("expected at least one saved turn")
	}
	if store.savedTurns[0].State != api.TurnStreaming {
		t.Errorf("pre-stream turn state = %d, want TurnStreaming", store.savedTurns[0].State)
	}
}
