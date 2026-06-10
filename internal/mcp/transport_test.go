package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStdioTransport_CommandNotFound(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("this-command-definitely-does-not-exist-12345")
	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected 'not found in PATH' error, got: %v", err)
	}
}

func TestStdioTransport_RoundTrip(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}'
read line
echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"test","description":"A test tool"}]}}'
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "helper.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	resp, err := tr.Send(ctx, "initialize", nil)
	if err != nil {
		t.Fatalf("send initialize: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %v", resp.Error)
	}

	resp2, err := tr.Send(ctx, "tools/list", nil)
	if err != nil {
		t.Fatalf("send tools/list: %v", err)
	}
	if resp2.Error != nil {
		t.Fatalf("tools/list returned error: %v", resp2.Error)
	}
}

func TestStdioTransport_SendTimeout(t *testing.T) {
	t.Parallel()

	// Reads the request then waits for another line that never comes.
	script := `#!/bin/sh
read line
read line
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "hang.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Send(ctx, "initialize", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "cancelled") && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestStdioTransport_Notify(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
read line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "notify.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	if err := tr.Notify(ctx, "notifications/initialized", nil); err != nil {
		t.Fatalf("notify: %v", err)
	}

	resp, err := tr.Send(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("send after notify: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestStdioTransport_AlreadyConnected(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while read line; do
  :
done
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	defer tr.Close()

	err := tr.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for double connect")
	}
	if !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("expected 'already connected' error, got: %v", err)
	}
}

func TestStdioTransport_SendBeforeConnect(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("echo")
	_, err := tr.Send(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected', got: %v", err)
	}
}

func TestStdioTransport_CloseBeforeConnect(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("echo")
	if err := tr.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStdioTransport_SendAfterClose(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while read line; do
  :
done
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop2.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := tr.Send(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected error after close")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected 'closed' error, got: %v", err)
	}
}
