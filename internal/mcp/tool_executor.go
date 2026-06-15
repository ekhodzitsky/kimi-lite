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
	logger     *slog.Logger
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(client api.MCPClient) *ToolExecutor {
	return &ToolExecutor{client: client, logger: slog.Default()}
}

// SetLogger sets the logger used by the executor. It is safe for concurrent use.
func (m *ToolExecutor) SetLogger(logger *slog.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

// Definitions returns available MCP tool definitions with names prefixed by "mcp_".
func (m *ToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tools, err := m.client.ListTools(ctx)
	if err != nil {
		m.mu.RLock()
		cached := m.cachedDefs
		logger := m.logger
		m.mu.RUnlock()
		if cached != nil {
			logger.Warn("mcp ListTools failed, returning cached tool definitions", "error", err)
			return cached
		}
		logger.Warn("mcp ListTools failed, no cached definitions available", "error", err)
		return nil
	}
	// Copy before mutating so callers cannot observe or modify the internal
	// cached slice through the returned value.
	copied := make([]api.ToolDefinition, len(tools))
	copy(copied, tools)
	readOnly := make(map[string]bool, len(copied))
	for i := range copied {
		readOnly[copied[i].Name] = copied[i].Annotations.ReadOnlyHint
		copied[i].Name = "mcp_" + copied[i].Name
	}
	m.mu.Lock()
	m.cachedDefs = copied
	m.readOnly = readOnly
	m.mu.Unlock()
	return copied
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
	if !strings.HasPrefix(call.Name, "mcp_") {
		return api.ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  fmt.Sprintf("mcp tool name must start with mcp_, got %q", call.Name),
		}, nil
	}
	name := strings.TrimPrefix(call.Name, "mcp_")
	var args map[string]any
	if call.Arguments == "" {
		args = map[string]any{}
	} else if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
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
