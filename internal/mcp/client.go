package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	clientName               = "kimi-lite"
	clientVersion            = "0.1.0"
	requestedProtocolVersion = "2024-11-05"
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

// NewClientFromServerConfig creates a new MCP client from a direct server
// configuration. It selects stdio or http transport based on cfg.Transport.
//
// Trust model: stdio transports execute the configured command with arguments
// directly. Only configure servers and commands you trust, because arbitrary
// command execution is possible. HTTP transports use the provided httpClient,
// or netutil.SecureHTTPClient() when nil, to harden outbound requests against
// SSRF.
func NewClientFromServerConfig(cfg api.MCPServerConfig, httpClient *http.Client) (*Client, error) {
	switch cfg.Transport {
	case api.MCPTransportStdio:
		tr := NewStdioTransport(cfg.Command, cfg.Args...)
		tr.SetEnv(cfg.Env)
		tr.SetCWD(cfg.CWD)
		return NewClient(tr), nil
	case api.MCPTransportHTTP:
		return NewClient(NewHTTPTransport(cfg.URL, cfg.Headers, cfg.BearerTokenEnvVar, httpClient)), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q", cfg.Transport)
	}
}

// Connect establishes connection to mcp-guard and performs the MCP initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("connect transport: %w", err)
	}

	initParams := map[string]any{
		"protocolVersion": requestedProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    clientName,
			"version": clientVersion,
		},
	}

	resp, err := c.transport.Send(ctx, "initialize", initParams)
	if err != nil {
		if closeErr := c.transport.Close(); closeErr != nil {
			slog.Warn("failed to close mcp transport after initialize error", "error", closeErr)
		}
		return fmt.Errorf("mcp initialize: %w", err)
	}

	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		if closeErr := c.transport.Close(); closeErr != nil {
			slog.Warn("failed to close mcp transport after initialize response decode error", "error", closeErr)
		}
		return fmt.Errorf("unmarshal initialize response: %w", err)
	}

	if initResult.ProtocolVersion == "" {
		if closeErr := c.transport.Close(); closeErr != nil {
			slog.Warn("failed to close mcp transport after empty protocol version", "error", closeErr)
		}
		return fmt.Errorf("mcp initialize: server returned empty protocol version")
	}
	// Accept the server's chosen version if it is the same as or older than
	// the version we requested. Reject newer versions explicitly because we
	// have not been implemented against them.
	if initResult.ProtocolVersion > requestedProtocolVersion {
		if closeErr := c.transport.Close(); closeErr != nil {
			slog.Warn("failed to close mcp transport after unsupported protocol version", "error", closeErr)
		}
		return fmt.Errorf("mcp initialize: unsupported protocol version %q", initResult.ProtocolVersion)
	}

	if err := c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		if closeErr := c.transport.Close(); closeErr != nil {
			slog.Warn("failed to close mcp transport after initialized notification error", "error", closeErr)
		}
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
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	resp, err := c.transport.Send(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("call tool %s: %w", name, err)
	}

	var result struct {
		Content []json.RawMessage `json:"content"`
		IsError bool              `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("unmarshal tools/call response: %w", err)
	}

	var output string
	for _, raw := range result.Content {
		var item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			slog.Warn("mcp tool returned malformed content item", "tool", name, "error", err)
			if output != "" {
				output += "\n"
			}
			output += "[malformed content item]"
			continue
		}
		switch item.Type {
		case "text":
			if output != "" {
				output += "\n"
			}
			output += item.Text
		default:
			if output != "" {
				output += "\n"
			}
			var withURI struct {
				Resource struct {
					URI string `json:"uri"`
				} `json:"resource"`
			}
			uri := ""
			if err := json.Unmarshal(raw, &withURI); err == nil && withURI.Resource.URI != "" {
				uri = withURI.Resource.URI
			}
			if uri != "" {
				output += "[" + item.Type + ": " + uri + "]"
			} else {
				output += "[" + item.Type + "]"
			}
		}
	}

	if result.IsError {
		if strings.TrimSpace(output) == "" {
			output = "tool returned an error with no text content"
		}
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
