package llm

import (
	"context"
	"log/slog"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// FallbackClient tries the primary LLM client first, then falls back on failure.
type FallbackClient struct {
	primary  api.LLMClient
	fallback api.LLMClient
}

// NewFallbackClient creates a composite LLM client with fallback support.
func NewFallbackClient(primary, fallback api.LLMClient) *FallbackClient {
	return &FallbackClient{primary: primary, fallback: fallback}
}

// Chat delegates to primary, then fallback on failure.
func (c *FallbackClient) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	msg, err := c.primary.Chat(ctx, messages, tools)
	if err != nil && c.fallback != nil {
		slog.Warn("primary LLM failed, trying fallback", "error", err)
		return c.fallback.Chat(ctx, messages, tools)
	}
	return msg, err
}

// ChatStream delegates to primary, then fallback on failure.
func (c *FallbackClient) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	stream, err := c.primary.ChatStream(ctx, messages, tools)
	if err != nil && c.fallback != nil {
		slog.Warn("primary LLM stream failed, trying fallback", "error", err)
		return c.fallback.ChatStream(ctx, messages, tools)
	}
	return stream, err
}

// Models delegates to primary (fallback not used for listing models).
func (c *FallbackClient) Models() []api.ModelInfo {
	return c.primary.Models()
}
