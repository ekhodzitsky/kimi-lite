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

	compressor := NewContextCompressor(llm)
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
	compressor := NewContextCompressor(llm)

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

	compressor := NewContextCompressor(llm)
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

	compressor := NewContextCompressor(llm)
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
