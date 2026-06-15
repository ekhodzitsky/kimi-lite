package core

import (
	"context"
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// CompositeToolExecutor combines multiple tool executors into a single executor.
// It unions tool definitions and routes execution to the correct child.
type CompositeToolExecutor struct {
	executors []api.ToolExecutor
}

// NewCompositeToolExecutor creates a new CompositeToolExecutor.
func NewCompositeToolExecutor(executors ...api.ToolExecutor) *CompositeToolExecutor {
	return &CompositeToolExecutor{
		executors: executors,
	}
}

// findExecutor returns the first child executor that advertises the named tool
// in its current Definitions. Callers should not cache the result because child
// definitions may change over time.
func (c *CompositeToolExecutor) findExecutor(ctx context.Context, name string) api.ToolExecutor {
	for _, exec := range c.executors {
		if exec == nil {
			continue
		}
		for _, def := range exec.Definitions(ctx) {
			if def.Name == name {
				return exec
			}
		}
	}
	return nil
}

// Definitions returns the union of all child definitions, deduplicated by name
// with first-seen ordering preserved.
func (c *CompositeToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition {
	defs := make([]api.ToolDefinition, 0, 16)
	seen := make(map[string]bool)
	for _, exec := range c.executors {
		if exec == nil {
			continue
		}
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

// Execute routes the tool call to the correct child executor. The routing is
// re-resolved on every call so the executor does not keep stale snapshots of
// child definitions.
func (c *CompositeToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	exec := c.findExecutor(ctx, call.Name)
	if exec == nil {
		return api.ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, nil
	}
	return exec.Execute(ctx, call)
}

// IsReadOnly delegates to the child executor that owns the named tool. The
// lookup is performed on demand because definitions may change.
func (c *CompositeToolExecutor) IsReadOnly(name string) bool {
	exec := c.findExecutor(context.Background(), name)
	if exec == nil {
		return false
	}
	return exec.IsReadOnly(name)
}
