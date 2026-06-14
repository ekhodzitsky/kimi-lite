package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// TestClientIntegration_WithFakeGuard exercises the full stdio MCP path against
// a fake guard binary built on demand from testdata/fake_guard.go.
func TestClientIntegration_WithFakeGuard(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available, cannot build fake guard")
	}

	binPath := filepath.Join(t.TempDir(), "fake_guard")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./testdata/fake_guard.go")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build fake guard: %v\n%s", err, out)
	}

	cfg := api.MCPConfig{
		GuardCommand: binPath,
		GuardConfig:  "",
	}
	client := NewClientFromConfig(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "fake_echo" {
		t.Errorf("expected tool name %q, got %q", "fake_echo", tools[0].Name)
	}

	output, err := client.CallTool(ctx, "fake_echo", map[string]any{"message": "ping"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if output != "pong" {
		t.Errorf("expected output %q, got %q", "pong", output)
	}
}
