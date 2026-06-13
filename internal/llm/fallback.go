package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	if err != nil && c.fallback != nil && !isClientError(err) {
		slog.Warn("primary LLM failed, trying fallback", "error", err)
		return c.fallback.Chat(ctx, messages, tools)
	}
	return msg, err
}

// isClientError reports whether err is a 4xx client error.
// Client errors should not trigger failover.
func isClientError(err error) bool {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsClientError()
	}
	return false
}

// ChatStream delegates to primary, then fallback on failure.
// It reads the primary stream until the first non-empty content chunk.
// If an error chunk arrives before any content has been emitted,
// and the error is not a 4xx client error, it falls back to the secondary client.
// Once content has been delivered, subsequent errors pass through unchanged.
// This provides an at-most-once-content failover guarantee.
func (c *FallbackClient) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	primaryCtx, cancelPrimary := context.WithCancel(ctx)
	stream, err := c.primary.ChatStream(primaryCtx, messages, tools)
	if err != nil {
		cancelPrimary()
		if c.fallback != nil && !isClientError(err) {
			slog.Warn("primary LLM stream failed, trying fallback", "error", err)
			return c.fallback.ChatStream(ctx, messages, tools)
		}
		return nil, err
	}

	// Peek the stream until we see the first non-empty content chunk or an error.
	// If the stream closes before either, fall back.
	for {
		select {
		case chunk, ok := <-stream:
			if !ok {
				cancelPrimary()
				if c.fallback != nil {
					slog.Warn("primary LLM stream closed without chunks, trying fallback")
					return c.fallback.ChatStream(ctx, messages, tools)
				}
				return nil, fmt.Errorf("primary LLM stream closed without data")
			}
			if chunk.Error != nil {
				cancelPrimary()
				if c.fallback != nil && !isClientError(chunk.Error) {
					slog.Warn("primary LLM stream returned pre-content error, trying fallback", "error", chunk.Error)
					return c.fallback.ChatStream(ctx, messages, tools)
				}
				return nil, chunk.Error
			}
			if chunk.Content != "" {
				// First content chunk is healthy — relay it and the rest through a new channel.
				out := make(chan api.StreamChunk, 64)
				go func(first api.StreamChunk) {
					defer close(out)
					defer cancelPrimary()
					select {
					case out <- first:
					case <-ctx.Done():
						return
					}
					for {
						select {
						case ch, ok := <-stream:
							if !ok {
								return
							}
							select {
							case out <- ch:
							case <-ctx.Done():
								return
							}
						case <-ctx.Done():
							return
						}
					}
				}(chunk)
				return out, nil
			}
			// Empty chunk (no content, no error) — keep reading.
		case <-time.After(5 * time.Second):
			cancelPrimary()
			if c.fallback != nil {
				slog.Warn("primary LLM stream idle on first chunk, trying fallback")
				return c.fallback.ChatStream(ctx, messages, tools)
			}
			return nil, fmt.Errorf("primary LLM stream idle timeout waiting for first chunk")
		case <-ctx.Done():
			cancelPrimary()
			return nil, ctx.Err()
		}
	}
}

// Models returns the union of primary and fallback model lists,
// deduplicated by ModelInfo.Name with primary-first ordering.
func (c *FallbackClient) Models() []api.ModelInfo {
	primaryModels := c.primary.Models()
	seen := make(map[string]struct{}, len(primaryModels))
	for _, m := range primaryModels {
		seen[m.Name] = struct{}{}
	}
	result := make([]api.ModelInfo, len(primaryModels))
	copy(result, primaryModels)

	if c.fallback != nil {
		for _, m := range c.fallback.Models() {
			if _, ok := seen[m.Name]; !ok {
				seen[m.Name] = struct{}{}
				result = append(result, m)
			}
		}
	}
	return result
}
