package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// mockMCPClient is a test double for api.MCPClient.
type mockMCPClient struct {
	listToolsFunc func(ctx context.Context) ([]api.ToolDefinition, error)
	callToolFunc  func(ctx context.Context, name string, args map[string]any) (string, error)
	closeFunc     func() error
}

func (m *mockMCPClient) Connect(ctx context.Context) error {
	return nil
}

func (m *mockMCPClient) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	if m.listToolsFunc != nil {
		return m.listToolsFunc(ctx)
	}
	return nil, nil
}

func (m *mockMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, name, args)
	}
	return "", nil
}

func (m *mockMCPClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestToolExecutor_Definitions(t *testing.T) {
	t.Parallel()

	t.Run("prefixes returned names", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				return []api.ToolDefinition{
					{Name: "read_file", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
					{Name: "grep", Description: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
				}, nil
			},
		}
		exec := NewToolExecutor(client)

		defs := exec.Definitions(context.Background())
		if len(defs) != 2 {
			t.Fatalf("len(defs) = %d, want 2", len(defs))
		}
		if defs[0].Name != "mcp_read_file" {
			t.Errorf("defs[0].Name = %q, want mcp_read_file", defs[0].Name)
		}
		if defs[1].Name != "mcp_grep" {
			t.Errorf("defs[1].Name = %q, want mcp_grep", defs[1].Name)
		}
	})

	t.Run("returns nil on ListTools error with no cache", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				return nil, errors.New("mcp unavailable")
			},
		}
		exec := NewToolExecutor(client)

		if defs := exec.Definitions(context.Background()); defs != nil {
			t.Fatalf("expected nil, got %+v", defs)
		}
	})

	t.Run("caches and falls back on ListTools error", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				callCount++
				if callCount == 1 {
					return []api.ToolDefinition{{Name: "read_file", Description: "Read a file"}}, nil
				}
				return nil, errors.New("network error")
			},
		}
		exec := NewToolExecutor(client)

		defs1 := exec.Definitions(context.Background())
		if len(defs1) != 1 || defs1[0].Name != "mcp_read_file" {
			t.Fatalf("first defs = %+v, want one mcp_read_file", defs1)
		}

		defs2 := exec.Definitions(context.Background())
		want := []api.ToolDefinition{{Name: "mcp_read_file", Description: "Read a file"}}
		if !reflect.DeepEqual(defs2, want) {
			t.Fatalf("cached defs = %+v, want %+v", defs2, want)
		}
	})

	t.Run("context cancellation returns promptly", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		exec := NewToolExecutor(client)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		defs := exec.Definitions(ctx)
		elapsed := time.Since(start)

		if defs != nil {
			t.Fatalf("expected nil definitions on cancelled context, got %v", defs)
		}
		if elapsed > 1*time.Second {
			t.Fatalf("expected prompt return on cancelled context, took %v", elapsed)
		}
	})
}

func TestToolExecutor_IsReadOnly(t *testing.T) {
	t.Parallel()

	t.Run("read-only hint is exposed", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				return []api.ToolDefinition{
					{Name: "read_file", Annotations: api.ToolAnnotations{ReadOnlyHint: true}},
					{Name: "write_file", Annotations: api.ToolAnnotations{ReadOnlyHint: false}},
				}, nil
			},
		}
		exec := NewToolExecutor(client)
		_ = exec.Definitions(context.Background())

		if !exec.IsReadOnly("mcp_read_file") {
			t.Error("expected mcp_read_file to be read-only")
		}
		if exec.IsReadOnly("mcp_write_file") {
			t.Error("expected mcp_write_file not to be read-only")
		}
	})

	t.Run("unknown tool returns false", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				return []api.ToolDefinition{{Name: "read_file", Annotations: api.ToolAnnotations{ReadOnlyHint: true}}}, nil
			},
		}
		exec := NewToolExecutor(client)
		_ = exec.Definitions(context.Background())

		if exec.IsReadOnly("mcp_unknown") {
			t.Error("expected unknown tool not to be read-only")
		}
	})

	t.Run("non-mcp prefix returns false", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				return []api.ToolDefinition{{Name: "read_file", Annotations: api.ToolAnnotations{ReadOnlyHint: true}}}, nil
			},
		}
		exec := NewToolExecutor(client)
		_ = exec.Definitions(context.Background())

		if exec.IsReadOnly("read_file") {
			t.Error("expected non-prefixed name not to be read-only")
		}
	})

	t.Run("falls back to cached read-only map", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		client := &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
				callCount++
				if callCount == 1 {
					return []api.ToolDefinition{
						{Name: "read_file", Annotations: api.ToolAnnotations{ReadOnlyHint: true}},
					}, nil
				}
				return nil, errors.New("network error")
			},
		}
		exec := NewToolExecutor(client)
		_ = exec.Definitions(context.Background())
		_ = exec.Definitions(context.Background())

		if !exec.IsReadOnly("mcp_read_file") {
			t.Error("expected cached read-only hint to survive ListTools error")
		}
	})
}

func TestToolExecutor_Definitions_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := []api.ToolDefinition{
		{Name: "read_file", Description: "Read"},
	}
	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
			return original, nil
		},
	}
	exec := NewToolExecutor(client)
	_ = exec.Definitions(context.Background())

	if original[0].Name != "read_file" {
		t.Fatalf("client's tool definition was mutated to %q", original[0].Name)
	}
}

func TestToolExecutor_SetLogger(t *testing.T) {
	t.Parallel()

	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
			return nil, errors.New("unavailable")
		},
	}
	exec := NewToolExecutor(client)
	exec.SetLogger(logger)

	defs := exec.Definitions(context.Background())
	if defs != nil {
		t.Fatalf("expected nil definitions, got %+v", defs)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.records) == 0 {
		t.Fatal("expected log record from custom logger")
	}
}

func TestToolExecutor_Execute(t *testing.T) {
	t.Parallel()

	t.Run("strips mcp_ prefix and returns output", func(t *testing.T) {
		t.Parallel()

		var calledName string
		var calledArgs map[string]any
		client := &mockMCPClient{
			callToolFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
				calledName = name
				calledArgs = args
				return "done", nil
			},
		}
		exec := NewToolExecutor(client)

		result, err := exec.Execute(context.Background(), api.ToolCall{
			ID:        "call-1",
			Name:      "mcp_read_file",
			Arguments: `{"path":"a.txt"}`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calledName != "read_file" {
			t.Errorf("CallTool name = %q, want read_file", calledName)
		}
		if calledArgs["path"] != "a.txt" {
			t.Errorf("CallTool args = %+v, want path=a.txt", calledArgs)
		}
		if result.CallID != "call-1" {
			t.Errorf("result.CallID = %q, want call-1", result.CallID)
		}
		if result.Name != "mcp_read_file" {
			t.Errorf("result.Name = %q, want mcp_read_file", result.Name)
		}
		if result.Output != "done" {
			t.Errorf("result.Output = %q, want done", result.Output)
		}
		if result.Error != "" {
			t.Errorf("result.Error = %q, want empty", result.Error)
		}
	})

	t.Run("invalid arguments return ToolResult.Error with nil Go error", func(t *testing.T) {
		t.Parallel()

		exec := NewToolExecutor(&mockMCPClient{})
		result, err := exec.Execute(context.Background(), api.ToolCall{
			ID:        "call-2",
			Name:      "mcp_bad",
			Arguments: `not json`,
		})
		if err != nil {
			t.Fatalf("expected nil Go error, got %v", err)
		}
		if result.CallID != "call-2" {
			t.Errorf("result.CallID = %q, want call-2", result.CallID)
		}
		if result.Name != "mcp_bad" {
			t.Errorf("result.Name = %q, want mcp_bad", result.Name)
		}
		if result.Error == "" {
			t.Fatal("expected non-empty result.Error")
		}
		if result.Output != "" {
			t.Errorf("result.Output = %q, want empty", result.Output)
		}
	})

	t.Run("CallTool error surfaces in ToolResult.Error with nil Go error", func(t *testing.T) {
		t.Parallel()

		client := &mockMCPClient{
			callToolFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
				return "", errors.New("tool crashed")
			},
		}
		exec := NewToolExecutor(client)

		result, err := exec.Execute(context.Background(), api.ToolCall{
			ID:        "call-3",
			Name:      "mcp_grep",
			Arguments: `{"q":"x"}`,
		})
		if err != nil {
			t.Fatalf("expected nil Go error, got %v", err)
		}
		if result.Error != "tool crashed" {
			t.Errorf("result.Error = %q, want tool crashed", result.Error)
		}
		if result.Output != "" {
			t.Errorf("result.Output = %q, want empty", result.Output)
		}
	})

	t.Run("non-mcp prefix returns error result", func(t *testing.T) {
		t.Parallel()

		exec := NewToolExecutor(&mockMCPClient{})
		result, err := exec.Execute(context.Background(), api.ToolCall{
			ID:        "call-4",
			Name:      "read_file",
			Arguments: `{"path":"a.txt"}`,
		})
		if err != nil {
			t.Fatalf("expected nil Go error, got %v", err)
		}
		if result.Error == "" {
			t.Fatal("expected non-empty result.Error")
		}
		if !strings.Contains(result.Error, "mcp_") {
			t.Errorf("expected mcp_ prefix error, got %q", result.Error)
		}
	})
}

func TestToolExecutor_Execute_EmptyArguments(t *testing.T) {
	t.Parallel()

	var calledName string
	var calledArgs map[string]any
	client := &mockMCPClient{
		callToolFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
			calledName = name
			calledArgs = args
			return "done", nil
		},
	}
	exec := NewToolExecutor(client)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call-empty",
		Name:      "mcp_hello",
		Arguments: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledName != "hello" {
		t.Errorf("CallTool name = %q, want hello", calledName)
	}
	if calledArgs == nil {
		t.Fatal("expected empty map, got nil")
	}
	if len(calledArgs) != 0 {
		t.Errorf("expected empty args, got %+v", calledArgs)
	}
	if result.Error != "" {
		t.Errorf("result.Error = %q, want empty", result.Error)
	}
}
