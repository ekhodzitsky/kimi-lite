package core

import (
	"context"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestCompositeToolExecutor_Definitions_Union(t *testing.T) {
	t.Parallel()
	exec1 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "tool_a", Description: "a"}},
	}
	exec2 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "tool_b", Description: "b"}},
	}
	composite := NewCompositeToolExecutor(exec1, exec2)
	defs := composite.Definitions(context.Background())
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["tool_a"] || !names["tool_b"] {
		t.Errorf("expected union of definitions, got: %v", defs)
	}
}

func TestCompositeToolExecutor_Execute_RoutesCorrectly(t *testing.T) {
	t.Parallel()
	exec1 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "tool_a", Description: "a"}},
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Output: "from_a"}, nil
		},
	}
	exec2 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "tool_b", Description: "b"}},
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Output: "from_b"}, nil
		},
	}
	composite := NewCompositeToolExecutor(exec1, exec2)

	result, err := composite.Execute(context.Background(), api.ToolCall{ID: "1", Name: "tool_a"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "from_a" {
		t.Errorf("expected output from_a, got %q", result.Output)
	}

	result, err = composite.Execute(context.Background(), api.ToolCall{ID: "2", Name: "tool_b"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "from_b" {
		t.Errorf("expected output from_b, got %q", result.Output)
	}
}

func TestCompositeToolExecutor_Execute_UnknownTool(t *testing.T) {
	t.Parallel()
	composite := NewCompositeToolExecutor()
	result, err := composite.Execute(context.Background(), api.ToolCall{ID: "1", Name: "unknown"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for unknown tool")
	}
}

// TestCompositeToolExecutor_Collision_FirstWins documents that when two children
// define the same tool name, the first registered executor wins for execution
// and Definitions() deduplicates to the first-seen definition.
func TestCompositeToolExecutor_Collision_FirstWins(t *testing.T) {
	t.Parallel()
	exec1 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "shared", Description: "first"}},
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Output: "first"}, nil
		},
	}
	exec2 := &mockToolExecutor{
		defs: []api.ToolDefinition{{Name: "shared", Description: "second"}},
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{CallID: call.ID, Output: "second"}, nil
		},
	}
	composite := NewCompositeToolExecutor(exec1, exec2)

	result, err := composite.Execute(context.Background(), api.ToolCall{ID: "1", Name: "shared"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "first" {
		t.Errorf("expected first registered to win, got %q", result.Output)
	}

	defs := composite.Definitions(context.Background())
	count := 0
	for _, d := range defs {
		if d.Name == "shared" {
			count++
		}
	}
	// Definitions deduplicates; only the first definition is present.
	if count != 1 {
		t.Errorf("expected 1 definition with name 'shared', got %d", count)
	}
}

func TestCompositeToolExecutor_Empty(t *testing.T) {
	t.Parallel()
	composite := NewCompositeToolExecutor()
	defs := composite.Definitions(context.Background())
	if len(defs) != 0 {
		t.Errorf("expected 0 definitions, got %d", len(defs))
	}
	result, err := composite.Execute(context.Background(), api.ToolCall{ID: "1", Name: "any"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for empty executor")
	}
}
