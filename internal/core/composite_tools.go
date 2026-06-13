package core

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// CompositeToolExecutor combines multiple tool executors into a single executor.
// It unions tool definitions and routes execution to the correct child.
type CompositeToolExecutor struct {
	executors []api.ToolExecutor
	toolMap   map[string]api.ToolExecutor
}

// NewCompositeToolExecutor creates a new CompositeToolExecutor.
func NewCompositeToolExecutor(executors ...api.ToolExecutor) *CompositeToolExecutor {
	c := &CompositeToolExecutor{
		executors: executors,
		toolMap:   make(map[string]api.ToolExecutor),
	}
	for _, exec := range executors {
		for _, def := range exec.Definitions(context.Background()) {
			if _, exists := c.toolMap[def.Name]; exists {
				slog.Warn("tool definition collision detected, keeping first registration", "tool", def.Name)
				continue
			}
			c.toolMap[def.Name] = exec
		}
	}
	return c
}

// Definitions returns the union of all child definitions, deduplicated by name
// with first-seen ordering preserved.
func (c *CompositeToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition {
	defs := make([]api.ToolDefinition, 0, 16)
	seen := make(map[string]bool)
	for _, exec := range c.executors {
		for _, def := range exec.Definitions(ctx) {
			if seen[def.Name] {
				continue
			}
			seen[def.Name] = true
			defs = append(defs, def)
		}
	}
	return defs
}

// Execute routes the tool call to the correct child executor.
func (c *CompositeToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	exec, ok := c.toolMap[call.Name]
	if !ok {
		return api.ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, nil
	}
	return exec.Execute(ctx, call)
}

// IsReadOnly delegates to the child executor that owns the named tool.
func (c *CompositeToolExecutor) IsReadOnly(name string) bool {
	exec, ok := c.toolMap[name]
	if !ok {
		return false
	}
	return exec.IsReadOnly(name)
}
