package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ContextCompressor summarizes conversation history using an LLM to reduce
// token usage. It implements the /compact command logic.
type ContextCompressor struct {
	llm api.LLMClient
}

// NewContextCompressor creates a new ContextCompressor.
func NewContextCompressor(llm api.LLMClient) *ContextCompressor {
	return &ContextCompressor{llm: llm}
}

// Compact sends older messages to the LLM for summarization and replaces the
// session message history with a summary system message plus the most recent
// messages.
//
// keepRecent is the number of most recent messages to preserve verbatim.
// It returns the number of messages that were summarized.
func (c *ContextCompressor) Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error) {
	if keepRecent < 0 {
		keepRecent = 0
	}

	messages, err := store.GetMessages(ctx, sessionID, 0)
	if err != nil {
		return 0, fmt.Errorf("get messages: %w", err)
	}

	if len(messages) <= keepRecent+1 {
		// Not enough messages to justify compaction.
		return 0, nil
	}

	toSummarize := messages[:len(messages)-keepRecent]
	recent := messages[len(messages)-keepRecent:]

	var conversation strings.Builder
	for _, msg := range toSummarize {
		fmt.Fprintf(&conversation, "%s: %s\n", msg.Role, msg.Content)
	}

	summaryPrompt := api.Message{
		Role:      api.RoleUser,
		Content:   fmt.Sprintf("Summarize the key facts and context from the following conversation. Be concise but preserve all important information, decisions, and context needed to continue the conversation:\n\n%s", conversation.String()),
		CreatedAt: time.Now().UTC(),
	}

	resp, err := c.llm.Chat(ctx, []api.Message{summaryPrompt}, nil)
	if err != nil {
		return 0, fmt.Errorf("summarize conversation: %w", err)
	}

	// Set summary timestamp strictly before the oldest recent message so
	// that GetMessages' ORDER BY created_at ASC preserves chronological order.
	summaryCreatedAt := time.Now().UTC().Add(-time.Second)
	if len(recent) > 0 && recent[0].CreatedAt.Before(summaryCreatedAt) {
		summaryCreatedAt = recent[0].CreatedAt.Add(-time.Millisecond)
	}
	summaryMsg := api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleSystem,
		Content:   fmt.Sprintf("Previous conversation summary: %s", resp.Content),
		CreatedAt: summaryCreatedAt,
	}
	newMessages := append([]api.Message{summaryMsg}, recent...)
	if err := store.ReplaceMessages(ctx, sessionID, newMessages); err != nil {
		return 0, fmt.Errorf("replace messages: %w", err)
	}

	return len(toSummarize), nil
}
