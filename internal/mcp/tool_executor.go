package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ToolExecutor wraps an api.MCPClient to implement api.ToolExecutor.
type ToolExecutor struct {
	client     api.MCPClient
	mu         sync.RWMutex
	cachedDefs []api.ToolDefinition
	readOnly   map[string]bool // original tool name -> readOnlyHint
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(client api.MCPClient) *ToolExecutor {
	return &ToolExecutor{client: client}
}

// Definitions returns available MCP tool definitions with names prefixed by "mcp_".
func (m *ToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tools, err := m.client.ListTools(ctx)
	if err != nil {
		m.mu.RLock()
		cached := m.cachedDefs
		m.mu.RUnlock()
		if cached != nil {
			slog.Warn("mcp ListTools failed, returning cached tool definitions", "error", err)
			return cached
		}
		slog.Warn("mcp ListTools failed, no cached definitions available", "error", err)
		return nil
	}
	readOnly := make(map[string]bool, len(tools))
	for i := range tools {
		readOnly[tools[i].Name] = tools[i].Annotations.ReadOnlyHint
		tools[i].Name = "mcp_" + tools[i].Name
	}
	m.mu.Lock()
	m.cachedDefs = tools
	m.readOnly = readOnly
	m.mu.Unlock()
	return tools
}

// IsReadOnly reports whether the named MCP tool is read-only.
// The tool name must be prefixed with "mcp_".
func (m *ToolExecutor) IsReadOnly(name string) bool {
	if !strings.HasPrefix(name, "mcp_") {
		return false
	}
	plain := strings.TrimPrefix(name, "mcp_")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readOnly[plain]
}

// Execute invokes an MCP tool. The tool name must be prefixed with "mcp_".
func (m *ToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	name := strings.TrimPrefix(call.Name, "mcp_")
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return api.ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  fmt.Sprintf("invalid arguments: %v", err),
		}, nil
	}
	output, err := m.client.CallTool(ctx, name, args)
	result := api.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
		Output: output,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}
