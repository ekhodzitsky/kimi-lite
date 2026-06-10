package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ToolExecutor wraps an api.MCPClient to implement api.ToolExecutor.
type ToolExecutor struct {
	client api.MCPClient
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(client api.MCPClient) *ToolExecutor {
	return &ToolExecutor{client: client}
}

// Definitions returns available MCP tool definitions with names prefixed by "mcp_".
func (m *ToolExecutor) Definitions() []api.ToolDefinition {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := m.client.ListTools(ctx)
	if err != nil {
		return nil
	}
	for i := range tools {
		tools[i].Name = "mcp_" + tools[i].Name
	}
	return tools
}

// Execute invokes an MCP tool. The tool name must be prefixed with "mcp_".
func (m *ToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	name := strings.TrimPrefix(call.Name, "mcp_")
	var args map[string]interface{}
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

// IsReadOnly returns false for all MCP tools (conservative default).
func (m *ToolExecutor) IsReadOnly(name string) bool {
	return false
}
