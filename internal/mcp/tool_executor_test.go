package mcp

import (
	"context"
	"errors"
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

func TestToolExecutor_Definitions_CachesOnFailure(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
			callCount++
			if callCount == 1 {
				return []api.ToolDefinition{
					{Name: "read_file", Description: "Read a file"},
				}, nil
			}
			return nil, errors.New("network error")
		},
	}

	exec := NewToolExecutor(client)

	// First call succeeds and populates the cache.
	defs1 := exec.Definitions(context.Background())
	if len(defs1) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs1))
	}
	if defs1[0].Name != "mcp_read_file" {
		t.Fatalf("expected prefixed name mcp_read_file, got %q", defs1[0].Name)
	}

	// Second call fails but returns cached definitions.
	defs2 := exec.Definitions(context.Background())
	if len(defs2) != 1 {
		t.Fatalf("expected 1 cached definition, got %d", len(defs2))
	}
	if defs2[0].Name != "mcp_read_file" {
		t.Fatalf("expected cached prefixed name mcp_read_file, got %q", defs2[0].Name)
	}
}

func TestToolExecutor_Definitions_ContextCancellation(t *testing.T) {
	t.Parallel()

	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]api.ToolDefinition, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	exec := NewToolExecutor(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	defs := exec.Definitions(ctx)
	elapsed := time.Since(start)

	if defs != nil {
		t.Fatalf("expected nil definitions on cancelled context, got %v", defs)
	}

	// Should return promptly (well before the 5s timeout).
	if elapsed > 1*time.Second {
		t.Fatalf("expected prompt return on cancelled context, took %v", elapsed)
	}
}
