package mcp

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type fakeMCPClient struct {
	connectErr error
	tools      []api.ToolDefinition
	listErr    error
	callResult string
	callErr    error
	closeErr   error
	closed     bool

	lastCallName string
	lastCallArgs map[string]any
}

func (f *fakeMCPClient) Connect(ctx context.Context) error {
	return f.connectErr
}

func (f *fakeMCPClient) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

func (f *fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	f.lastCallName = name
	f.lastCallArgs = args
	return f.callResult, f.callErr
}

func (f *fakeMCPClient) Close() error {
	f.closed = true
	return f.closeErr
}

func TestMultiClient_ListTools_AggregatesAndDisambiguates(t *testing.T) {
	t.Parallel()

	alpha := &fakeMCPClient{
		tools: []api.ToolDefinition{
			{Name: "read_file", Description: "Read from alpha"},
			{Name: "grep", Description: "Search alpha"},
		},
	}
	beta := &fakeMCPClient{
		tools: []api.ToolDefinition{
			{Name: "read_file", Description: "Read from beta"},
			{Name: "list_directory", Description: "List beta"},
		},
	}

	clients := map[string]api.MCPClient{"alpha": alpha, "beta": beta}
	configs := map[string]api.MCPServerConfig{
		"alpha": {Enabled: true, Transport: api.MCPTransportStdio, Command: "alpha"},
		"beta":  {Enabled: true, Transport: api.MCPTransportStdio, Command: "beta"},
	}

	multi := NewMultiClient(clients, configs)
	tools, err := multi.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}

	names := make([]string, len(tools))
	for i, tt := range tools {
		names[i] = tt.Name
	}
	sort.Strings(names)
	want := []string{"beta_read_file", "grep", "list_directory", "read_file"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}

	// read_file should route to alpha (first seen); beta_read_file to beta.
	if _, err := multi.CallTool(context.Background(), "read_file", nil); err != nil {
		t.Fatalf("CallTool read_file error: %v", err)
	}
	if alpha.lastCallName != "read_file" {
		t.Errorf("alpha called with %q, want read_file", alpha.lastCallName)
	}

	if _, err := multi.CallTool(context.Background(), "beta_read_file", nil); err != nil {
		t.Fatalf("CallTool beta_read_file error: %v", err)
	}
	if beta.lastCallName != "read_file" {
		t.Errorf("beta called with %q, want read_file", beta.lastCallName)
	}
}

func TestMultiClient_ListTools_FiltersPerServer(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{
		tools: []api.ToolDefinition{
			{Name: "a", Description: "tool a"},
			{Name: "b", Description: "tool b"},
			{Name: "c", Description: "tool c"},
		},
	}

	cfg := api.MCPServerConfig{
		Enabled:       true,
		Transport:     api.MCPTransportStdio,
		Command:       "cmd",
		EnabledTools:  []string{"a", "c"},
		DisabledTools: []string{"c"},
	}

	multi := NewMultiClient(map[string]api.MCPClient{"srv": client}, map[string]api.MCPServerConfig{"srv": cfg})
	tools, err := multi.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "a" {
		t.Fatalf("tools = %v, want [a]", tools)
	}
}

func TestMultiClient_Connect_CombinesErrors(t *testing.T) {
	t.Parallel()

	good := &fakeMCPClient{}
	bad := &fakeMCPClient{connectErr: errors.New("refused")}

	clients := map[string]api.MCPClient{"good": good, "bad": bad}
	configs := map[string]api.MCPServerConfig{
		"good": {Enabled: true, Transport: api.MCPTransportStdio, Command: "good"},
		"bad":  {Enabled: true, Transport: api.MCPTransportStdio, Command: "bad"},
	}

	multi := NewMultiClient(clients, configs)
	err := multi.Connect(context.Background())
	if err == nil {
		t.Fatal("expected combined connect error")
	}
	if !errors.Is(err, bad.connectErr) && err.Error() != "connect mcp server bad: refused" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMultiClient_CallTool_RequiresListTools(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{}
	multi := NewMultiClient(map[string]api.MCPClient{"srv": client}, map[string]api.MCPServerConfig{"srv": {}})

	_, err := multi.CallTool(context.Background(), "anything", nil)
	if err == nil {
		t.Fatal("expected error when routing map not initialized")
	}
}

func TestMultiClient_CallTool_Timeout(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{
		tools:      []api.ToolDefinition{{Name: "tool", Description: "A tool"}},
		callResult: "ok",
	}
	cfg := api.MCPServerConfig{
		Enabled:       true,
		Transport:     api.MCPTransportStdio,
		Command:       "cmd",
		ToolTimeoutMs: 100,
	}
	multi := NewMultiClient(map[string]api.MCPClient{"srv": client}, map[string]api.MCPServerConfig{"srv": cfg})
	if _, err := multi.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := multi.CallTool(ctx, "tool", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if got != "ok" {
		t.Errorf("result = %q, want ok", got)
	}
	if client.lastCallName != "tool" {
		t.Errorf("called %q, want tool", client.lastCallName)
	}
}

func TestMultiClient_Close_ClosesAll(t *testing.T) {
	t.Parallel()

	alpha := &fakeMCPClient{}
	beta := &fakeMCPClient{}
	multi := NewMultiClient(
		map[string]api.MCPClient{"alpha": alpha, "beta": beta},
		map[string]api.MCPServerConfig{"alpha": {}, "beta": {}},
	)

	if err := multi.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if !alpha.closed || !beta.closed {
		t.Fatal("expected all clients to be closed")
	}
}

func TestMultiClient_ListTools_PartialFailure(t *testing.T) {
	t.Parallel()

	good := &fakeMCPClient{
		tools: []api.ToolDefinition{{Name: "ok", Description: "OK"}},
	}
	bad := &fakeMCPClient{listErr: errors.New("unavailable")}

	multi := NewMultiClient(
		map[string]api.MCPClient{"good": good, "bad": bad},
		map[string]api.MCPServerConfig{"good": {}, "bad": {}},
	)

	tools, err := multi.ListTools(context.Background())
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ok" {
		t.Fatalf("expected one tool from good server, got: %+v", tools)
	}
}

func TestMultiClient_ListTools_AllFail(t *testing.T) {
	t.Parallel()

	bad := &fakeMCPClient{listErr: errors.New("unavailable")}
	multi := NewMultiClient(
		map[string]api.MCPClient{"bad": bad},
		map[string]api.MCPServerConfig{"bad": {}},
	)

	_, err := multi.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error when all servers fail")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected unavailable error, got: %v", err)
	}
}

func TestMultiClient_CallTool_Error(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{
		tools:      []api.ToolDefinition{{Name: "tool", Description: "A tool"}},
		callResult: "",
		callErr:    errors.New("tool failed"),
	}
	multi := NewMultiClient(
		map[string]api.MCPClient{"srv": client},
		map[string]api.MCPServerConfig{"srv": {Enabled: true, Transport: api.MCPTransportStdio, Command: "cmd"}},
	)
	if _, err := multi.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	_, err := multi.CallTool(context.Background(), "tool", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("expected tool failed error, got: %v", err)
	}
}

func TestMultiClient_Connect_Timeout(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{
		connectErr: context.DeadlineExceeded,
	}
	multi := NewMultiClient(
		map[string]api.MCPClient{"srv": client},
		map[string]api.MCPServerConfig{"srv": {Enabled: true, Transport: api.MCPTransportStdio, Command: "cmd", StartupTimeoutMs: 1}},
	)

	err := multi.Connect(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got: %v", err)
	}
}

func TestMultiClient_Close_Error(t *testing.T) {
	t.Parallel()

	alpha := &fakeMCPClient{}
	beta := &fakeMCPClient{closeErr: errors.New("beta close failed")}
	multi := NewMultiClient(
		map[string]api.MCPClient{"alpha": alpha, "beta": beta},
		map[string]api.MCPServerConfig{"alpha": {}, "beta": {}},
	)

	err := multi.Close()
	if err == nil {
		t.Fatal("expected combined close error")
	}
	if !strings.Contains(err.Error(), "beta close failed") {
		t.Fatalf("expected beta close error, got: %v", err)
	}
}

func TestMultiClient_CallTool_NoTimeoutError(t *testing.T) {
	t.Parallel()

	client := &fakeMCPClient{
		tools:      []api.ToolDefinition{{Name: "tool", Description: "A tool"}},
		callResult: "",
		callErr:    errors.New("tool failed"),
	}
	multi := NewMultiClient(
		map[string]api.MCPClient{"srv": client},
		map[string]api.MCPServerConfig{"srv": {Enabled: true, Transport: api.MCPTransportStdio, Command: "cmd", ToolTimeoutMs: 0}},
	)
	if _, err := multi.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	_, err := multi.CallTool(context.Background(), "tool", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("expected tool failed error, got: %v", err)
	}
}

func TestMultiClient_CallTool_MissingClient(t *testing.T) {
	t.Parallel()

	multi := NewMultiClient(
		map[string]api.MCPClient{},
		map[string]api.MCPServerConfig{},
	)
	// Manually seed the routing map so the missing-client check is exercised.
	multi.mu.Lock()
	multi.routes = map[string]string{"missing": "missing"}
	multi.origins = map[string]string{"missing": "missing"}
	multi.mu.Unlock()

	_, err := multi.CallTool(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing client error, got: %v", err)
	}
}
