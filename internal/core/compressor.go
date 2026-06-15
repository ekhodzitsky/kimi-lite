package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	// summaryPromptOverhead accounts for the framing and instructions in the
	// summarization prompt itself.
	summaryPromptOverhead = 50

	// summarySizeEstimateBase is the fixed token cost of the generated summary
	// message (prefix + per-message overhead).
	summarySizeEstimateBase = 50

	// summarySizeEstimateDivisor converts the summarization input token count
	// into an estimated summary size.
	summarySizeEstimateDivisor = 10

	// boundedInputDivisor derives the maximum summarization input budget from
	// the overall context window.
	boundedInputDivisor = 2
)

// ContextCompressor summarizes conversation history using an LLM to reduce
// token usage. It implements the /compact command logic.
type ContextCompressor struct {
	llm           api.LLMClient
	contextWindow int
	timeout       time.Duration
	estimator     api.TokenEstimator
	mu            sync.Mutex
}

// NewContextCompressor creates a new ContextCompressor.
func NewContextCompressor(llm api.LLMClient, contextWindow int, timeout time.Duration) (*ContextCompressor, error) {
	if contextWindow < 0 {
		return nil, fmt.Errorf("contextWindow must be non-negative")
	}
	if timeout < 0 {
		return nil, fmt.Errorf("timeout must be non-negative")
	}
	return &ContextCompressor{
		llm:           llm,
		contextWindow: contextWindow,
		timeout:       timeout,
	}, nil
}

// SetTokenEstimator replaces the default len/4 estimator. A nil estimator is
// ignored. The estimator is protected by a mutex and is safe to call before
// Compact, but SetTokenEstimator and Compact must not be called concurrently.
func (c *ContextCompressor) SetTokenEstimator(est api.TokenEstimator) {
	if est == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.estimator = est
}

// estimateTokens returns a token estimate using the configured estimator or a
// len/4 fallback. The estimator access is synchronized.
func (c *ContextCompressor) estimateTokens(msgs []api.Message) int {
	c.mu.Lock()
	est := c.estimator
	c.mu.Unlock()
	if est != nil {
		return est.Estimate(msgs)
	}
	return estimateTokensLegacy(msgs)
}

// estimateTokensLegacy returns a rough token estimate using a len/4 heuristic,
// rounding up so that small strings are not counted as zero tokens.
func estimateTokensLegacy(msgs []api.Message) int {
	tokens := 0
	for _, m := range msgs {
		tokens += (len(m.Content) + 3) / 4
		for _, tc := range m.ToolCalls {
			tokens += (len(tc.Name) + 3) / 4
			tokens += (len(tc.Arguments) + 3) / 4
			tokens += 10
		}
		tokens += 3 // per-message overhead
	}
	return tokens
}

// formatMessageForSummary renders a message including tool calls and results
// so that summarization does not lose tool-call fidelity.
func formatMessageForSummary(msg api.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s", msg.Role)
	if !isToolResult(msg) {
		fmt.Fprintf(&b, ": %s", msg.Content)
	}
	for _, tc := range msg.ToolCalls {
		fmt.Fprintf(&b, "\n  tool_call: %s(%s)", tc.Name, tc.Arguments)
	}
	if isToolResult(msg) {
		fmt.Fprintf(&b, "\n  tool_result for call %s: %s", msg.ToolCallID, msg.Content)
	}
	b.WriteString("\n")
	return b.String()
}

// isToolResult reports whether a message carries a tool result.
func isToolResult(msg api.Message) bool {
	return msg.Role == api.RoleTool || msg.ToolCallID != ""
}

// isAssistantWithToolCalls reports whether a message is an assistant message
// that requested one or more tool calls.
func isAssistantWithToolCalls(msg api.Message) bool {
	return msg.Role == api.RoleAssistant && len(msg.ToolCalls) > 0
}

// splitLeadingSystem splits the leading consecutive RoleSystem messages from
// the rest of the conversation.
func splitLeadingSystem(messages []api.Message) ([]api.Message, []api.Message) {
	i := 0
	for i < len(messages) && messages[i].Role == api.RoleSystem {
		i++
	}
	return messages[:i], messages[i:]
}

// findSafeBoundary returns the index in messages where the "recent" region
// should begin. It walks leftwards from len(messages)-keepRecent while the
// boundary would fall inside an assistant{ToolCalls}+results group, ensuring
// that compaction never splits such a pair.
func findSafeBoundary(messages []api.Message, keepRecent int) int {
	if keepRecent < 0 {
		keepRecent = 0
	}
	if keepRecent >= len(messages) {
		return 0
	}

	boundary := len(messages) - keepRecent
	for boundary > 0 && boundary < len(messages) &&
		(isToolResult(messages[boundary]) || isAssistantWithToolCalls(messages[boundary])) {
		boundary--
	}
	return boundary
}

// selectWithinBudget returns the newest suffix of messages that fits within
// maxTokens, walking backwards until adding another message would exceed the
// budget. At least one message is always returned so the LLM has something to
// summarize.
func (c *ContextCompressor) selectWithinBudget(messages []api.Message, maxTokens int) []api.Message {
	var included []api.Message
	tokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgTokens := c.estimateTokens([]api.Message{msg})
		if tokens+msgTokens > maxTokens && len(included) > 0 {
			break
		}
		included = append(included, msg)
		tokens += msgTokens
	}
	// Reverse to chronological order.
	for i, j := 0, len(included)-1; i < j; i, j = i+1, j-1 {
		included[i], included[j] = included[j], included[i]
	}
	return included
}

// Compact sends older messages to the LLM for summarization and replaces the
// session message history with leading system messages, a summary system
// message, and the most recent messages.
//
// keepRecent is the target number of most recent non-system messages to
// preserve verbatim. The actual boundary is pair-aware: it walks backwards so
// that assistant/tool-call groups are never split across the summary/recent
// boundary. It returns the number of messages actually sent to the LLM for
// summarization (excluding leading system messages).
func (c *ContextCompressor) Compact(ctx context.Context, store api.MessageStore, sessionID string, keepRecent int) (int, error) {
	if c == nil || c.llm == nil {
		return 0, fmt.Errorf("context compressor LLM client is nil")
	}
	if keepRecent < 0 {
		keepRecent = 0
	}

	messages, err := store.GetMessages(ctx, sessionID, 0)
	if err != nil {
		return 0, fmt.Errorf("get messages: %w", err)
	}

	// Gate compaction on token estimate vs context window.
	totalTokens := c.estimateTokens(messages)
	if c.contextWindow > 0 {
		threshold := c.contextWindow / boundedInputDivisor
		if totalTokens <= threshold {
			return 0, nil
		}
	}

	// Preserve leading system/identity prompts outside the summarization.
	leading, rest := splitLeadingSystem(messages)

	if len(rest) <= keepRecent+1 {
		// Not enough non-system messages to justify compaction.
		return 0, nil
	}

	boundary := findSafeBoundary(rest, keepRecent)
	middle := rest[:boundary]
	recent := rest[boundary:]

	if len(middle) == 0 {
		// Nothing to summarize after respecting pair-aware boundary.
		return 0, nil
	}

	// Determine which middle messages actually fit in the summarization budget.
	var included []api.Message
	if c.contextWindow > 0 {
		maxInputTokens := c.contextWindow / boundedInputDivisor
		if maxInputTokens > summaryPromptOverhead {
			maxInputTokens -= summaryPromptOverhead
		} else {
			maxInputTokens = 0
		}
		included = c.selectWithinBudget(middle, maxInputTokens)
	} else {
		included = middle
	}

	// Short-circuit when the estimated summary+recent would not be smaller
	// than the originals. Use the bounded input so the estimate is accurate.
	if c.contextWindow > 0 {
		toSummarizeTokens := c.estimateTokens(included)
		recentTokens := c.estimateTokens(recent)
		leadingTokens := c.estimateTokens(leading)
		summaryTokens := summarySizeEstimateBase + toSummarizeTokens/summarySizeEstimateDivisor
		if leadingTokens+summaryTokens+recentTokens >= totalTokens {
			return 0, nil
		}
	}

	var conversation strings.Builder
	for _, msg := range included {
		conversation.WriteString(formatMessageForSummary(msg))
	}

	summaryPrompt := api.Message{
		Role:      api.RoleUser,
		Content:   fmt.Sprintf("Summarize the key facts and context from the following conversation. Be concise but preserve all important information, decisions, and context needed to continue the conversation:\n\n%s", conversation.String()),
		CreatedAt: time.Now().UTC(),
	}

	// Time-box the summarization LLM call.
	chatCtx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		chatCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	resp, err := c.llm.Chat(chatCtx, []api.Message{summaryPrompt}, nil)
	if err != nil {
		return 0, fmt.Errorf("summarize conversation: %w", err)
	}

	// Reject empty/whitespace summaries before the destructive ReplaceMessages.
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return 0, fmt.Errorf("summarization produced empty output")
	}

	// Set summary timestamp strictly after the last leading system message
	// and strictly before the oldest recent message so that GetMessages'
	// ORDER BY created_at ASC preserves chronological order.
	summaryCreatedAt := time.Now().UTC()
	if len(recent) > 0 {
		summaryCreatedAt = recent[0].CreatedAt.Add(-time.Nanosecond)
	}
	if len(leading) > 0 {
		lastLeading := leading[len(leading)-1].CreatedAt
		if !summaryCreatedAt.After(lastLeading) {
			summaryCreatedAt = lastLeading.Add(time.Nanosecond)
		}
	}
	// If leading and recent timestamps collide at nanosecond precision, bump
	// all affected recent messages forward so the summary remains strictly
	// before every preserved recent message and order is preserved. This is
	// safe because ReplaceMessages persists the adjusted slice.
	if len(recent) > 0 {
		next := summaryCreatedAt
		for i := range recent {
			if !recent[i].CreatedAt.After(next) {
				next = next.Add(time.Nanosecond)
				recent[i].CreatedAt = next
			} else {
				next = recent[i].CreatedAt
			}
		}
	}
	summaryMsg := api.Message{
		ID:        idgen.GenerateID(),
		Role:      api.RoleSystem,
		Content:   fmt.Sprintf("Previous conversation summary: %s", resp.Content),
		CreatedAt: summaryCreatedAt,
	}
	newMessages := make([]api.Message, 0, len(leading)+1+len(recent))
	newMessages = append(newMessages, leading...)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, recent...)
	if err := store.ReplaceMessages(ctx, sessionID, newMessages); err != nil {
		return 0, fmt.Errorf("replace messages: %w", err)
	}

	dropped := len(middle) - len(included)
	if dropped > 0 {
		slog.Warn("dropped old messages from summarization input due to token budget",
			"dropped", dropped,
			"included", len(included),
			"total", len(middle))
	}

	return len(included), nil
}
