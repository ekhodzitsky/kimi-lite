package subagents

import (
	"context"
	"fmt"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// defaultSubagentTimeout is used when a request does not specify one.
const defaultSubagentTimeout = 60 * time.Second

// defaultSubagentMaxRounds is used when a request does not specify one.
const defaultSubagentMaxRounds = 10

// runLoop executes an ephemeral LLM↔tool loop for the given subagent config.
func runLoop(ctx context.Context, r *Runner, cfg agentConfig, req api.SubagentRequest) (*api.SubagentResult, error) {
	start := now()

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultSubagentTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultSubagentMaxRounds
	}

	messages := []api.Message{
		{Role: api.RoleSystem, Content: cfg.systemPrompt, CreatedAt: start},
		{Role: api.RoleUser, Content: req.Prompt, CreatedAt: start},
	}

	allowed := cfg.tools
	if len(req.AllowedTools) > 0 {
		allowed = intersect(allowed, req.AllowedTools)
	}
	defs := filterDefinitions(r.toolExecutor.Definitions(ctx), allowed)

	for round := 0; round < maxRounds; round++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("subagent cancelled: %w", ctx.Err())
		default:
		}

		msg, err := r.llmClient.Chat(ctx, messages, defs)
		if err != nil {
			return nil, fmt.Errorf("subagent llm chat: %w", err)
		}
		messages = append(messages, *msg)

		if len(msg.ToolCalls) == 0 {
			return &api.SubagentResult{
				Output:   msg.Content,
				Rounds:   round + 1,
				Duration: time.Since(start),
			}, nil
		}

		for _, tc := range msg.ToolCalls {
			result, err := r.toolExecutor.Execute(ctx, tc)
			if err != nil {
				result = api.ToolResult{CallID: tc.ID, Name: tc.Name, Error: err.Error()}
			}
			messages = append(messages, api.Message{
				Role:       api.RoleTool,
				Content:    toolResultContent(result),
				ToolCallID: result.CallID,
				CreatedAt:  now(),
			})
		}
	}

	return nil, fmt.Errorf("subagent exceeded max rounds (%d)", maxRounds)
}

// toolResultContent formats a tool result as a tool message.
func toolResultContent(result api.ToolResult) string {
	if result.Error == "" {
		return result.Output
	}
	if result.Output == "" {
		return "Error: " + result.Error
	}
	return result.Output + "\nError: " + result.Error
}

// intersect returns the elements of a that are also in b, preserving a's order.
func intersect(a, b []string) []string {
	want := make(map[string]struct{}, len(b))
	for _, n := range b {
		want[n] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, n := range a {
		if _, ok := want[n]; ok {
			out = append(out, n)
		}
	}
	return out
}
