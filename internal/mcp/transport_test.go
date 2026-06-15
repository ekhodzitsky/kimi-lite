package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
cat >/dev/null
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
cat >/dev/null
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
	script := `#!/bin/sh
echo "diagnostic message" >&2
sleep 0.2
exit 1
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fail.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("unexpected Connect error: %v", err)
	}
	defer tr.Close()

	// Give the readLoop time to observe the subprocess exit and capture stderr.
	time.Sleep(200 * time.Millisecond)

	_, err := tr.Send(ctx, "ping", nil)
	if err == nil {
		t.Fatal("expected error after subprocess exit")
	}
	if !strings.Contains(err.Error(), "diagnostic message") {
		t.Fatalf("expected stderr in error, got: %v", err)
	}
}

func TestStdioTransport_OversizedFrame(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available, cannot build oversized-frame helper")
	}

	helperSrc := `package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	var buf [1024]byte
	_, _ = os.Stdin.Read(buf[:])
	payload := strings.Repeat("A", 9*1024*1024)
	fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"x\":\"%s\"}}\n", payload)
	time.Sleep(30 * time.Second)
}
`
	tmpDir := t.TempDir()
	helperSrcPath := filepath.Join(tmpDir, "hugeframe.go")
	helperBinPath := filepath.Join(tmpDir, "hugeframe")
	if err := os.WriteFile(helperSrcPath, []byte(helperSrc), 0644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	buildCmd := exec.Command("go", "build", "-o", helperBinPath, helperSrcPath)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}

	tr := NewStdioTransport(helperBinPath)
	tr.closeTimeout = 100 * time.Millisecond
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

func TestStdioTransport_CloseKillsOnTimeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix signals required")
	}

	// `sleep` exits on SIGTERM, so the graceful-wait path should terminate it
	// quickly once the configured grace period expires.
	tr := NewStdioTransport("sleep", "30")
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

	if elapsed > 2*time.Second {
		t.Fatalf("Close took too long: %v", elapsed)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("Close returned before grace period: %v", elapsed)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("expected child process to be killed")
	}
}

func TestStdioTransport_ConnectStartFailureCleanup(t *testing.T) {
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

	// After a failed start the transport should be cleanly unusable, not
	// half-connected.
	_, err = tr.Send(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected Send error after failed Connect")
	}
	if !strings.Contains(err.Error(), "not connected") && !strings.Contains(err.Error(), "transport closed") {
		t.Fatalf("expected not-connected/closed error, got: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("close after failed connect: %v", err)
	}
}

func TestStdioTransport_DecodeErrorBroadcast(t *testing.T) {
	script := `#!/bin/sh
read line
echo 'this is not json'
cat >/dev/null
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "badjson.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()

	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	// Send blocks until the readLoop broadcasts the decode error.
	_, err := tr.Send(ctx, "ping", nil)
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "parse error") && !strings.Contains(err.Error(), "-32700") {
		t.Fatalf("expected parse error, got: %v", err)
	}

	// readErr should now be set, so a follow-up send fast-fails.
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = tr.Send(ctx2, "ping", nil)
	if err == nil {
		t.Fatal("expected fast-fail error after decode error")
	}
	if !strings.Contains(err.Error(), "parse error") &&
		!strings.Contains(err.Error(), "decode JSON-RPC response") &&
		!strings.Contains(err.Error(), "transport closed") {
		t.Fatalf("expected cached readErr or closed error, got: %v", err)
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
	script := `#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":99,"result":{}}'
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
sleep 0.1
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

func TestStdioTransport_CommandNotFound_MentionsCommand(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("definitely-missing-command-12345")
	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "definitely-missing-command-12345") {
		t.Fatalf("expected error to mention command name, got: %v", err)
	}
}

func TestMinimalEnv_ExpandedSecretPatterns(t *testing.T) {
	// Cannot run in parallel because it modifies process environment.
	t.Setenv("MY_APP_TOKEN", "secret")
	t.Setenv("MY_PRIVATE_KEY", "secret")
	t.Setenv("MY_SESSION_COOKIE", "secret")
	t.Setenv("MY_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("PATH", "/usr/bin")

	out := minimalEnv()
	for _, e := range out {
		key, _, _ := strings.Cut(e, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PRIVATE_KEY") ||
			strings.Contains(upper, "COOKIE") || strings.Contains(upper, "OAUTH") {
			t.Fatalf("minimalEnv should exclude secret-like key %q", key)
		}
	}

	hasPath := false
	for _, e := range out {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Fatal("minimalEnv should preserve PATH")
	}
}

func TestStdioTransport_EnvOrderIsStable(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh")
	tr.SetEnv(map[string]string{
		"Z_VAR": "z",
		"A_VAR": "a",
		"M_VAR": "m",
	})

	env := tr.buildEnv()
	var keys []string
	for _, e := range env {
		if k, _, ok := strings.Cut(e, "="); ok {
			keys = append(keys, k)
		}
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] < keys[i-1] {
			t.Fatalf("environment is not sorted: %v", keys)
		}
	}
	if !slices.Contains(keys, "A_VAR") || !slices.Contains(keys, "M_VAR") || !slices.Contains(keys, "Z_VAR") {
		t.Fatalf("expected custom env keys, got: %v", keys)
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

func TestJSONRPCError_Error(t *testing.T) {
	t.Parallel()

	var nilErr *JSONRPCError
	if got := nilErr.Error(); got != "" {
		t.Fatalf("nil JSONRPCError.Error() = %q, want empty", got)
	}

	err := &JSONRPCError{Code: -32600, Message: "bad request"}
	if got := err.Error(); !strings.Contains(got, "JSON-RPC error -32600") {
		t.Fatalf("expected error message to contain code, got: %q", got)
	}
}

func TestBoundedBuffer_String(t *testing.T) {
	t.Parallel()

	b := newBoundedBuffer(4)
	if got := b.String(); got != "" {
		t.Fatalf("empty buffer String() = %q, want empty", got)
	}

	// Partial fill: String should return bytes in order.
	_, _ = b.Write([]byte("ab"))
	if got := b.String(); got != "ab" {
		t.Fatalf("partial buffer String() = %q, want ab", got)
	}

	// Fill exactly to capacity.
	_, _ = b.Write([]byte("cd"))
	if got := b.String(); got != "abcd" {
		t.Fatalf("full buffer String() = %q, want abcd", got)
	}

	// Overfill: String should be the last cap bytes in order.
	_, _ = b.Write([]byte("ef"))
	if got := b.String(); got != "cdef" {
		t.Fatalf("wrapped buffer String() = %q, want cdef", got)
	}
}

func TestStdioTransport_SetCWD(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while read line; do
  :
done
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	tr.SetCWD(tmpDir)

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	tr.mu.Lock()
	gotDir := tr.cmd.Dir
	tr.mu.Unlock()
	if gotDir != tmpDir {
		t.Fatalf("cmd.Dir = %q, want %q", gotDir, tmpDir)
	}
}

func TestStdioTransport_Connect_ImmediateExitSuccess(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	_ = tr.Close()
}

func TestStdioTransport_Connect_CwdMissing(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	tr.SetCWD(filepath.Join(t.TempDir(), "missing"))
	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing cwd")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Fatalf("expected start error, got: %v", err)
	}
}

func TestStdioTransport_Connect_StdinPipeError(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	tr.newCmd = func(name string, arg ...string) *exec.Cmd {
		cmd := exec.Command(name, arg...)
		cmd.Stdin = &bytes.Buffer{}
		return cmd
	}

	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create stdin pipe") {
		t.Fatalf("expected stdin pipe error, got: %v", err)
	}
}

func TestStdioTransport_Connect_StdoutPipeError(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	tr.newCmd = func(name string, arg ...string) *exec.Cmd {
		cmd := exec.Command(name, arg...)
		cmd.Stdout = &bytes.Buffer{}
		return cmd
	}

	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create stdout pipe") {
		t.Fatalf("expected stdout pipe error, got: %v", err)
	}
}

func TestStdioTransport_IncompleteFrame(t *testing.T) {
	script := `#!/bin/sh
read line
printf '{"jsonrpc":"2.0","id":1,"result":{'
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "partial.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	tr.closeTimeout = 100 * time.Millisecond
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	_, err := tr.Send(ctx, "ping", nil)
	if err == nil {
		t.Fatal("expected error for incomplete frame")
	}
	if !strings.Contains(err.Error(), "parse error") && !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected parse/incomplete error, got: %v", err)
	}
}

func TestStdioTransport_Send_MarshalError(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while read line; do
  :
done
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	_, err := tr.Send(context.Background(), "bad", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got: %v", err)
	}
}

func TestStdioTransport_Send_ResponseError(t *testing.T) {
	script := `#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request"}}'
cat >/dev/null
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "err.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	resp, err := tr.Send(ctx, "bad", nil)
	if err == nil {
		t.Fatal("expected JSON-RPC error")
	}
	if resp == nil || resp.Error == nil {
		t.Fatal("expected response with error")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected bad request error, got: %v", err)
	}
}

func TestStdioTransport_Notify_MarshalError(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while read line; do
  :
done
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	err := tr.Notify(context.Background(), "bad", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got: %v", err)
	}
}

func TestStdioTransport_Notify_Closed(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	err := tr.Notify(context.Background(), "ping", nil)
	if err == nil {
		t.Fatal("expected error after close")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed error, got: %v", err)
	}
}

func TestStdioTransport_Notify_NotConnected(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	err := tr.Notify(context.Background(), "ping", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected not connected error, got: %v", err)
	}
}

func TestStdioTransport_Notify_ContextCancel(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("sleep", "3600")
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := tr.Notify(ctx, "ping", map[string]string{"x": strings.Repeat("a", 1024*1024)})
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

func TestStdioTransport_Close_DoubleClose(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("/bin/sh", "-c", "exit 0")
	if err := tr.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got: %v", err)
	}
}

func TestStdioTransport_Send_WriteError(t *testing.T) {
	t.Parallel()

	// Close stdin immediately and keep stdout open so the write fails.
	script := `#!/bin/sh
exec 0<&-
while :; do sleep 1; done
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "closedstdin.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	tr.closeTimeout = 100 * time.Millisecond
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	// Give the subprocess time to close stdin before we try to write.
	time.Sleep(100 * time.Millisecond)

	_, err := tr.Send(context.Background(), "ping", nil)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write request") {
		t.Fatalf("expected write request error, got: %v", err)
	}
}

func TestStdioTransport_Notify_WriteError(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
exec 0<&-
while :; do sleep 1; done
`
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "closedstdin2.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tr := NewStdioTransport("/bin/sh", scriptPath)
	tr.closeTimeout = 100 * time.Millisecond
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer tr.Close()

	// Give the subprocess time to close stdin before we try to write.
	time.Sleep(100 * time.Millisecond)

	err := tr.Notify(context.Background(), "ping", nil)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write notification") {
		t.Fatalf("expected write notification error, got: %v", err)
	}
}

func TestStdioTransport_Close_WithPendingSend(t *testing.T) {
	t.Parallel()

	tr := NewStdioTransport("sleep", "3600")
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = tr.Send(context.Background(), "ping", nil)
	}()

	time.Sleep(50 * time.Millisecond)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pending Send did not return after Close")
	}
}
