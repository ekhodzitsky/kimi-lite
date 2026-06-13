package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestContextCompressor_Compact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Seed messages.
	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("message %d", i),
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{
				Role:      api.RoleAssistant,
				Content:   "summary of conversation",
				CreatedAt: time.Now().UTC(),
			}, nil
		},
	}

	compressor := NewContextCompressor(llm, 0, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 3 {
		t.Errorf("summarized = %d, want 3", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 3 { // 1 summary + 2 recent
		t.Fatalf("expected 3 messages after compact, got %d", len(msgs))
	}
	if msgs[0].Role != api.RoleSystem {
		t.Errorf("msg[0].role = %q, want system", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "summary of conversation") {
		t.Errorf("summary missing expected content: %q", msgs[0].Content)
	}
}

func TestContextCompressor_Compact_TooFewMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: time.Now().UTC()})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hi", CreatedAt: time.Now().UTC()})

	llm := &mockLLMClient{}
	compressor := NewContextCompressor(llm, 0, 0)

	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 0 {
		t.Errorf("summarized = %d, want 0", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestContextCompressor_Compact_LLMError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("msg %d", i),
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return nil, fmt.Errorf("llm overloaded")
		},
	}

	compressor := NewContextCompressor(llm, 0, 0)
	_, err := compressor.Compact(ctx, store, sess.ID, 1)
	if err == nil {
		t.Fatal("expected error for LLM failure")
	}
	if !strings.Contains(err.Error(), "llm overloaded") {
		t.Errorf("error = %q, want llm overloaded", err.Error())
	}
}

func TestContextCompressor_Compact_SummaryBeforeRecent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Seed messages with explicit timestamps to simulate real ordering.
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("message %d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Role: api.RoleAssistant, Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 0, 0)
	_, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != api.RoleSystem {
		t.Fatalf("expected summary first, got role %q", msgs[0].Role)
	}
	// Summary must be strictly before the oldest recent message.
	if !msgs[0].CreatedAt.Before(msgs[1].CreatedAt) {
		t.Errorf("summary created_at %v not before recent[0] created_at %v", msgs[0].CreatedAt, msgs[1].CreatedAt)
	}
}

func TestContextCompressor_Compact_TokenGate_NoCompaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   "short",
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			t.Error("LLM should not be called when token gate prevents compaction")
			return nil, fmt.Errorf("unexpected")
		},
	}

	compressor := NewContextCompressor(llm, 10000, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 0 {
		t.Errorf("summarized = %d, want 0", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages preserved, got %d", len(msgs))
	}
}

func TestContextCompressor_Compact_TokenGate_DoesCompact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Enough content to exceed contextWindow/2 = 500 tokens.
	content := strings.Repeat("a", 300) // ~75 tokens each
	for i := 0; i < 10; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   content,
			CreatedAt: time.Now().UTC(),
		})
	}

	called := false
	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			called = true
			return &api.Message{Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 1000, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized == 0 {
		t.Error("expected compaction to occur")
	}
	if !called {
		t.Error("expected LLM to be called")
	}
}

func TestContextCompressor_Compact_BoundedInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Create many long messages; with contextWindow=2000, only a subset should be sent.
	content := strings.Repeat("x", 400) // ~100 tokens each
	for i := 0; i < 20; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("msg%d %s", i, content),
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			prompt := messages[0].Content
			count := strings.Count(prompt, "msg")
			// With maxInputTokens=950, each message is ~100+ tokens, so at most 9 fit.
			if count > 9 {
				return nil, fmt.Errorf("too many messages in prompt: %d", count)
			}
			return &api.Message{Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 2000, 0)
	_, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
}

func TestContextCompressor_Compact_Timeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
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
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			if _, ok := ctx.Deadline(); !ok {
				return nil, fmt.Errorf("expected context to have a deadline")
			}
			return &api.Message{Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 1000, 30*time.Second)
	_, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
}

func TestContextCompressor_Compact_EmptySummary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
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
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "   "}, nil
		},
	}

	compressor := NewContextCompressor(llm, 500, 0)
	_, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
	if !strings.Contains(err.Error(), "summarization produced empty output") {
		t.Errorf("error = %q, want empty summary error", err.Error())
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 5 {
		t.Fatalf("expected original 5 messages preserved, got %d", len(msgs))
	}
}

func TestContextCompressor_Compact_PreservesLeadingSystemPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	systemMsg := api.Message{
		ID:      "sys1",
		Role:    api.RoleSystem,
		Content: "You are a helpful coding assistant.",
	}
	_ = store.AppendMessage(ctx, sess.ID, systemMsg)

	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("message %d", i),
			CreatedAt: base.Add(time.Duration(i+1) * time.Minute),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 0, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 3 {
		t.Errorf("summarized = %d, want 3", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 4 { // 1 leading system + 1 summary + 2 recent
		t.Fatalf("expected 4 messages after compact, got %d", len(msgs))
	}
	if msgs[0].ID != systemMsg.ID {
		t.Errorf("leading system message ID = %q, want %q", msgs[0].ID, systemMsg.ID)
	}
	if msgs[0].Role != api.RoleSystem {
		t.Errorf("msgs[0].role = %q, want system", msgs[0].Role)
	}
	if msgs[0].Content != systemMsg.Content {
		t.Errorf("msgs[0].content = %q, want %q", msgs[0].Content, systemMsg.Content)
	}
	if msgs[1].Role != api.RoleSystem {
		t.Errorf("msgs[1].role = %q, want system (summary)", msgs[1].Role)
	}
	if !msgs[0].CreatedAt.Before(msgs[1].CreatedAt) {
		t.Errorf("leading system created_at %v not strictly before summary created_at %v", msgs[0].CreatedAt, msgs[1].CreatedAt)
	}
	if !msgs[1].CreatedAt.Before(msgs[2].CreatedAt) {
		t.Errorf("summary created_at %v not strictly before recent[0] created_at %v", msgs[1].CreatedAt, msgs[2].CreatedAt)
	}
}

func TestContextCompressor_Compact_PairAwareBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	base := time.Now().UTC().Add(-time.Hour)
	seed := []api.Message{
		{ID: "m1", Role: api.RoleUser, Content: "first", CreatedAt: base},
		{ID: "m2", Role: api.RoleUser, Content: "second", CreatedAt: base.Add(time.Minute)},
		{ID: "m3", Role: api.RoleUser, Content: "third", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "m4", Role: api.RoleAssistant, Content: "I'll read it", ToolCalls: []api.ToolCall{{ID: "tc1", Name: "read_file", Arguments: `{"path":"a.go"}`}}, CreatedAt: base.Add(3 * time.Minute)},
		{ID: "m5", Role: api.RoleTool, Content: "package main", ToolCallID: "tc1", CreatedAt: base.Add(4 * time.Minute)},
		{ID: "m6", Role: api.RoleUser, Content: "what next", CreatedAt: base.Add(5 * time.Minute)},
	}
	for _, msg := range seed {
		_ = store.AppendMessage(ctx, sess.ID, msg)
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "summary"}, nil
		},
	}

	// keepRecent=2 would place the raw boundary at index 4 (the tool result),
	// splitting the assistant/tool-call pair. The pair-aware boundary must
	// walk backwards to keep the pair intact.
	compressor := NewContextCompressor(llm, 0, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 2 {
		t.Errorf("summarized = %d, want 2", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 5 { // 1 summary + 4 recent
		t.Fatalf("expected 5 messages after compact, got %d", len(msgs))
	}

	// Verify no orphaned tool results and no dangling tool_calls anywhere in
	// the preserved message list.
	if hasDanglingToolCalls(msgs) {
		t.Errorf("messages contain dangling tool_calls: %+v", msgs)
	}
	if hasOrphanedToolResult(msgs) {
		t.Errorf("messages contain orphaned tool result: %+v", msgs)
	}

	// Verify the assistant and its tool result are preserved together.
	var gotAssistant, gotTool bool
	for _, msg := range msgs {
		if msg.ID == "m4" {
			gotAssistant = true
		}
		if msg.ID == "m5" {
			gotTool = true
		}
	}
	if !gotAssistant || !gotTool {
		t.Errorf("expected assistant m4 and tool result m5 to be preserved together, got assistant=%v tool=%v", gotAssistant, gotTool)
	}
}

func TestContextCompressor_Compact_PairAwareBoundary_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	base := time.Now().UTC().Add(-time.Hour)
	seed := []api.Message{
		{ID: "m1", Role: api.RoleUser, Content: "first", CreatedAt: base},
		{ID: "m2", Role: api.RoleUser, Content: "second", CreatedAt: base.Add(time.Minute)},
		{ID: "m3", Role: api.RoleAssistant, Content: "I'll read and write", ToolCalls: []api.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: `{"path":"a.go"}`},
			{ID: "tc2", Name: "write_file", Arguments: `{"path":"b.go"}`},
		}, CreatedAt: base.Add(2 * time.Minute)},
		{ID: "m4", Role: api.RoleTool, Content: "package a", ToolCallID: "tc1", CreatedAt: base.Add(3 * time.Minute)},
		{ID: "m5", Role: api.RoleTool, Content: "done", ToolCallID: "tc2", CreatedAt: base.Add(4 * time.Minute)},
		{ID: "m6", Role: api.RoleUser, Content: "what next", CreatedAt: base.Add(5 * time.Minute)},
	}
	for _, msg := range seed {
		_ = store.AppendMessage(ctx, sess.ID, msg)
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "summary"}, nil
		},
	}

	// keepRecent=2 would place the raw boundary at index 4 (inside the tool
	// results). The pair-aware boundary must walk back before the assistant
	// message that emitted the tool calls.
	compressor := NewContextCompressor(llm, 0, 0)
	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized != 1 {
		t.Errorf("summarized = %d, want 1", summarized)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 6 { // 1 summary + 5 recent
		t.Fatalf("expected 6 messages after compact, got %d", len(msgs))
	}

	if hasDanglingToolCalls(msgs) {
		t.Errorf("messages contain dangling tool_calls: %+v", msgs)
	}
	if hasOrphanedToolResult(msgs) {
		t.Errorf("messages contain orphaned tool result: %+v", msgs)
	}

	var gotAssistant, gotTool1, gotTool2 bool
	for _, msg := range msgs {
		switch msg.ID {
		case "m3":
			gotAssistant = true
		case "m4":
			gotTool1 = true
		case "m5":
			gotTool2 = true
		}
	}
	if !gotAssistant || !gotTool1 || !gotTool2 {
		t.Errorf("expected assistant m3 and both tool results to be preserved together, got assistant=%v tool1=%v tool2=%v", gotAssistant, gotTool1, gotTool2)
	}
}

func TestContextCompressor_Compact_SummaryTimestampBetweenLeadingAndRecent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	// Use the exact same timestamp for the leading system prompt and the
	// oldest recent message to regression-test timestamp ordering.
	shared := time.Now().UTC().Add(-time.Hour)
	systemMsg := api.Message{
		ID:        "sys1",
		Role:      api.RoleSystem,
		Content:   "You are a helpful coding assistant.",
		CreatedAt: shared,
	}
	_ = store.AppendMessage(ctx, sess.ID, systemMsg)

	_ = store.AppendMessage(ctx, sess.ID, api.Message{
		ID:        "m0",
		Role:      api.RoleUser,
		Content:   "zeroth",
		CreatedAt: shared.Add(-time.Minute),
	})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{
		ID:        "m1",
		Role:      api.RoleUser,
		Content:   "first",
		CreatedAt: shared.Add(time.Minute),
	})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{
		ID:        "m2",
		Role:      api.RoleUser,
		Content:   "second",
		CreatedAt: shared, // collides with leading system timestamp
	})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{
		ID:        "m3",
		Role:      api.RoleAssistant,
		Content:   "third",
		CreatedAt: shared.Add(2 * time.Minute),
	})

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Content: "summary"}, nil
		},
	}

	compressor := NewContextCompressor(llm, 0, 0)
	_, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 4 { // 1 leading system + 1 summary + 2 recent
		t.Fatalf("expected 4 messages after compact, got %d", len(msgs))
	}
	if msgs[0].ID != systemMsg.ID {
		t.Errorf("leading system message ID = %q, want %q", msgs[0].ID, systemMsg.ID)
	}
	if msgs[1].Role != api.RoleSystem {
		t.Errorf("msgs[1].role = %q, want system (summary)", msgs[1].Role)
	}
	if msgs[2].ID != "m2" {
		t.Errorf("recent[0] ID = %q, want m2", msgs[2].ID)
	}
	if !msgs[0].CreatedAt.Before(msgs[1].CreatedAt) {
		t.Errorf("leading system created_at %v not strictly before summary created_at %v", msgs[0].CreatedAt, msgs[1].CreatedAt)
	}
	if !msgs[1].CreatedAt.Before(msgs[2].CreatedAt) {
		t.Errorf("summary created_at %v not strictly before recent[0] created_at %v", msgs[1].CreatedAt, msgs[2].CreatedAt)
	}
}

func hasDanglingToolCalls(msgs []api.Message) bool {
	for i, msg := range msgs {
		if msg.Role != api.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			found := false
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].ToolCallID == tc.ID {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
	}
	return false
}

func hasOrphanedToolResult(msgs []api.Message) bool {
	for _, msg := range msgs {
		if msg.Role != api.RoleTool && msg.ToolCallID == "" {
			continue
		}
		found := false
		for _, other := range msgs {
			if other.Role == api.RoleAssistant {
				for _, tc := range other.ToolCalls {
					if tc.ID == msg.ToolCallID {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// scalingEstimator returns len(msgs) * factor tokens.
type scalingEstimator struct {
	factor int
}

func (s *scalingEstimator) Estimate(msgs []api.Message) int { return len(msgs) * s.factor }

func TestContextCompressor_SetTokenEstimator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	for i := 0; i < 10; i++ {
		_ = store.AppendMessage(ctx, sess.ID, api.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("message %d", i),
			CreatedAt: time.Now().UTC(),
		})
	}

	llm := &mockLLMClient{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return &api.Message{Role: api.RoleAssistant, Content: "summary", CreatedAt: time.Now().UTC()}, nil
		},
	}

	// With contextWindow=1000 and an estimator that reports 100 tokens per
	// message, the threshold (500) is exceeded and compaction should run.
	compressor := NewContextCompressor(llm, 1000, 0)
	compressor.SetTokenEstimator(&scalingEstimator{factor: 100})

	summarized, err := compressor.Compact(ctx, store, sess.ID, 2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summarized == 0 {
		t.Error("expected compaction to run with high token estimate")
	}
}
