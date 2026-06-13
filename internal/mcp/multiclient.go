package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// MultiClient implements api.MCPClient by aggregating multiple underlying MCP
// clients. It is used when the user configures direct MCP servers via
// cfg.MCPServers.
type MultiClient struct {
	clients map[string]api.MCPClient
	configs map[string]api.MCPServerConfig

	mu      sync.RWMutex
	routes  map[string]string // final tool name -> server key
	origins map[string]string // final tool name -> original tool name
	tools   []api.ToolDefinition
}

// NewMultiClient creates a multi-client from a map of server-key to client and
// the corresponding server configurations.
func NewMultiClient(clients map[string]api.MCPClient, configs map[string]api.MCPServerConfig) *MultiClient {
	return &MultiClient{
		clients: clients,
		configs: configs,
	}
}

// Connect connects each underlying client sequentially. Errors are collected
// and returned as a combined error; clients that connected successfully remain
// connected.
func (m *MultiClient) Connect(ctx context.Context) error {
	var errs []error
	for name, cli := range m.clients {
		cfg := m.configs[name]
		timeout := time.Duration(cfg.StartupTimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		if err := cli.Connect(cctx); err != nil {
			errs = append(errs, fmt.Errorf("connect mcp server %s: %w", name, err))
		}
		cancel()
	}
	return errors.Join(errs...)
}

// ListTools aggregates tool definitions from all underlying clients. Tools are
// filtered by per-server EnabledTools/DisabledTools. Duplicate tool names are
// disambiguated by prefixing the duplicate with its server key.
func (m *MultiClient) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	sort.Strings(names)

	all := make([]api.ToolDefinition, 0)
	seen := make(map[string]string) // original tool name -> first server key
	routes := make(map[string]string)
	origins := make(map[string]string)

	for _, name := range names {
		cli := m.clients[name]
		cfg := m.configs[name]

		timeout := time.Duration(cfg.ToolTimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		tools, err := cli.ListTools(cctx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("list tools from mcp server %s: %w", name, err)
		}

		for _, t := range tools {
			if !toolAllowed(t.Name, cfg.EnabledTools, cfg.DisabledTools) {
				continue
			}
			finalName := t.Name
			if first, ok := seen[t.Name]; ok {
				finalName = name + "_" + t.Name
				// If the first occurrence of this name is still un-prefixed,
				// make sure it remains routable under its original name.
				_ = first
			} else {
				seen[t.Name] = name
			}
			routes[finalName] = name
			origins[finalName] = t.Name
			t.Name = finalName
			all = append(all, t)
		}
	}

	m.mu.Lock()
	m.routes = routes
	m.origins = origins
	m.tools = all
	m.mu.Unlock()

	return all, nil
}

// CallTool routes the tool call to the underlying client that owns it. The
// routing map is built by ListTools, so ListTools must be called before the
// first tool invocation.
func (m *MultiClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	m.mu.RLock()
	routes := m.routes
	origins := m.origins
	m.mu.RUnlock()

	if routes == nil {
		return "", fmt.Errorf("tool routing map not initialized; call ListTools first")
	}

	server, ok := routes[name]
	if !ok {
		return "", fmt.Errorf("tool %s not found in routing map", name)
	}

	cli := m.clients[server]
	cfg := m.configs[server]
	original := origins[name]

	timeout := time.Duration(cfg.ToolTimeoutMs) * time.Millisecond
	if timeout > 0 {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return cli.CallTool(cctx, original, args)
	}
	return cli.CallTool(ctx, original, args)
}

// Close closes all underlying clients and returns a combined error.
func (m *MultiClient) Close() error {
	var errs []error
	for name, cli := range m.clients {
		if err := cli.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close mcp server %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func toolAllowed(name string, enabled, disabled []string) bool {
	if len(enabled) > 0 {
		found := false
		for _, e := range enabled {
			if e == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, d := range disabled {
		if d == name {
			return false
		}
	}
	return true
}
