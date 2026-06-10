package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	clientName    = "kimi-lite"
	clientVersion = "0.1.0"
)

// Client implements api.MCPClient using a JSON-RPC transport.
type Client struct {
	transport Transport
}

// NewClient creates a new MCP client with the given transport.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

// NewClientFromConfig creates a new MCP client from configuration.
// It uses stdio transport connected to mcp-guard, passing GuardConfig
// as the first positional argument to the command.
func NewClientFromConfig(cfg api.MCPConfig) *Client {
	transport := NewStdioTransport(cfg.GuardCommand, cfg.GuardConfig)
	return NewClient(transport)
}

// Connect establishes connection to mcp-guard and performs the MCP initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("connect transport: %w", err)
	}

	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    clientName,
			"version": clientVersion,
		},
	}

	resp, err := c.transport.Send(ctx, "initialize", initParams)
	if err != nil {
		_ = c.transport.Close()
		return fmt.Errorf("mcp initialize: %w", err)
	}

	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		_ = c.transport.Close()
		return fmt.Errorf("unmarshal initialize response: %w", err)
	}

	if err := c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		_ = c.transport.Close()
		return fmt.Errorf("mcp initialized notification: %w", err)
	}

	return nil
}

// ListTools returns available MCP tools.
func (c *Client) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	resp, err := c.transport.Send(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	var result struct {
		Tools []api.ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools/list response: %w", err)
	}

	return result.Tools, nil
}

// CallTool invokes an MCP tool with the given name and arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	resp, err := c.transport.Send(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("call tool %s: %w", name, err)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("unmarshal tools/call response: %w", err)
	}

	var output string
	for _, c := range result.Content {
		if c.Type == "text" {
			output += c.Text
		}
	}

	if result.IsError {
		return output, fmt.Errorf("tool %s returned error: %s", name, output)
	}

	return output, nil
}

// Close closes the MCP connection.
func (c *Client) Close() error {
	if err := c.transport.Close(); err != nil {
		return fmt.Errorf("close transport: %w", err)
	}
	return nil
}
