package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
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

func TestStdioTransport_MinimalEnv(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
env | sort
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "env.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	// The script prints env and exits; readLoop will get EOF.
	// Give it a moment to finish.
	time.Sleep(100 * time.Millisecond)

	// Verify PATH is present in the minimal env.
	env := minimalEnv()
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Fatal("minimalEnv missing PATH")
	}

	// Verify a secret-like variable is excluded.
	for _, e := range env {
		if strings.Contains(strings.ToUpper(e), "API_KEY") {
			t.Fatalf("minimalEnv should exclude secret keys, got: %s", e)
		}
	}
}

func TestStdioTransport_StderrCaptured(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
echo "diagnostic message" >&2
exit 1
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fail.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	err := tr.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for failing child")
	}
	if !strings.Contains(err.Error(), "diagnostic message") {
		t.Fatalf("expected stderr in error, got: %v", err)
	}
}

func TestStdioTransport_OversizedFrame(t *testing.T) {
	t.Parallel()

	// Script that prints a frame larger than maxFrameSize (8 MB).
	script := `#!/bin/sh
read line
# Print a JSON object with a huge string value.
printf '{"jsonrpc":"2.0","id":1,"result":{"x":"'
python3 -c "print('A' * 9000000, end='')"
printf '"}}\n'
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "huge.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	_, err := tr.Send(ctx, "initialize", nil)
	if err == nil {
		t.Fatal("expected error for oversized frame")
	}
	if !strings.Contains(err.Error(), "frame") && !strings.Contains(err.Error(), "size") {
		t.Fatalf("expected frame size error, got: %v", err)
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

func TestStdioTransport_CloseKillsAfterGracePeriod(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix signals required")
	}

	tmpDir := t.TempDir()
	helperSrc := filepath.Join(tmpDir, "ignoreterm.go")
	helperBin := filepath.Join(tmpDir, "ignoreterm")
	src := `package main

import (
	"fmt"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(syscall.SIGTERM)
	fmt.Println("ready")
	time.Sleep(30 * time.Second)
}
`
	if err := os.WriteFile(helperSrc, []byte(src), 0644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	buildCmd := exec.Command("go", "build", "-o", helperBin, helperSrc)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}

	tr := NewStdioTransport(helperBin)
	tr.closeTimeout = 50 * time.Millisecond
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	tr.mu.Lock()
	pid := tr.cmd.Process.Pid
	tr.mu.Unlock()

	start := time.Now()
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	elapsed := time.Since(start)

	// Should wait roughly the configured grace period, not the full 5s default.
	if elapsed > 2*time.Second {
		t.Fatalf("Close took too long: %v", elapsed)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("Close returned before grace period: %v", elapsed)
	}

	// Process should have been reaped; signal 0 will fail if it is gone.
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("expected child process to be killed")
	}
}

func TestStdioTransport_EOFFastFail(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "eof.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	// First send: child reads and exits, causing EOF.
	_, err := tr.Send(ctx, "initialize", nil)
	if err == nil {
		t.Fatal("expected error after EOF")
	}
	if !strings.Contains(err.Error(), "transport closed") {
		t.Fatalf("expected transport closed error, got: %v", err)
	}

	// Second send should fast-fail without blocking.
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = tr.Send(ctx2, "ping", nil)
	if err == nil {
		t.Fatal("expected fast-fail error")
	}
	if !strings.Contains(err.Error(), "transport closed") && !strings.Contains(err.Error(), "subprocess closed") {
		t.Fatalf("expected transport closed error, got: %v", err)
	}
}

func TestStdioTransport_SendWriteContextCancellation(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("sleep", "3600")
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	ctx2, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := tr.Send(ctx2, "initialize", map[string]string{"x": strings.Repeat("a", 1024*1024)})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cancelled") && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context cancellation error, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected fast return on cancellation, took %v", elapsed)
	}
}

func TestStdioTransport_CloseRaceNotify(t *testing.T) {
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
		t.Fatalf("connect: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				err := tr.Notify(ctx, "notifications/initialized", nil)
				if err == nil {
					continue
				}
				if strings.Contains(err.Error(), "file already closed") {
					t.Errorf("got 'file already closed', expected 'transport closed' or success: %v", err)
				}
			}
		}()
	}

	time.Sleep(5 * time.Millisecond)
	_ = tr.Close()
	wg.Wait()
}

func TestStdioTransport_JSONRPCRequestID(t *testing.T) {
	t.Parallel()

	req := JSONRPCRequest{JSONRPC: "2.0", ID: 0, Method: "test"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if !strings.Contains(string(data), `"id":0`) {
		t.Fatalf("expected id=0 in JSON, got: %s", string(data))
	}

	notify := JSONRPCNotification{JSONRPC: "2.0", Method: "test"}
	data, err = json.Marshal(notify)
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	if strings.Contains(string(data), `"id"`) {
		t.Fatalf("expected no id in notification JSON, got: %s", string(data))
	}
}

type testLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *testLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}
func (h *testLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(string) slog.Handler      { return h }

func TestStdioTransport_UnmatchedFrameDebugLog(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":99,"result":{}}'
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "unmatched.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	handler := &testLogHandler{}
	tr.logger = slog.New(handler)

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	// Send a request with id=1; the script replies id=99 first (unmatched), then id=1.
	resp, err := tr.Send(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	var found bool
	for _, r := range handler.records {
		if r.Message != "dropping unmatched JSON-RPC frame" {
			continue
		}
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "id" {
				switch v := a.Value.Any().(type) {
				case int64:
					if v == 99 {
						found = true
					}
				case int:
					if v == 99 {
						found = true
					}
				}
			}
			return true
		})
	}
	if !found {
		t.Fatal("expected debug log for unmatched id=99")
	}
}

func TestStdioTransport_StartFailureCleanup(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("echo")
	tr.newCmd = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("/nonexistent-binary-for-test")
	}

	ctx := context.Background()
	err := tr.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for failing start")
	}

	err = tr.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for second connect")
	}
	if strings.Contains(err.Error(), "already connected") {
		t.Fatalf("expected not 'already connected', got: %v", err)
	}
}

func TestIsClosedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EOF", io.EOF, true},
		{"ErrClosed", os.ErrClosed, true},
		{"file already closed", errors.New("read |0: file already closed"), true},
		{"other error", errors.New("something else"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isClosedError(tt.err); got != tt.want {
				t.Errorf("isClosedError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
