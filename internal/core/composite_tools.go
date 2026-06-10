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
		for _, def := range exec.Definitions() {
			if _, exists := c.toolMap[def.Name]; exists {
				slog.Warn("tool definition collision detected", "tool", def.Name)
			}
			c.toolMap[def.Name] = exec
		}
	}
	return c
}

// Definitions returns the union of all child definitions.
func (c *CompositeToolExecutor) Definitions() []api.ToolDefinition {
	var defs []api.ToolDefinition
	for _, exec := range c.executors {
		defs = append(defs, exec.Definitions()...)
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

// IsReadOnly delegates to the child executor that owns the tool.
func (c *CompositeToolExecutor) IsReadOnly(name string) bool {
	exec, ok := c.toolMap[name]
	if !ok {
		return false
	}
	return exec.IsReadOnly(name)
}
