package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// newTestExecutor creates a BuiltInToolExecutor for tests, failing the test
// if construction fails.
func newTestExecutor(t *testing.T, cfg ToolExecutorConfig) *BuiltInToolExecutor {
	t.Helper()
	exec, err := NewBuiltInToolExecutor(cfg)
	if err != nil {
		t.Fatalf("NewBuiltInToolExecutor: %v", err)
	}
	return exec
}

func TestNewBuiltInToolExecutor_DefaultTimeout(t *testing.T) {
	t.Parallel()
	exec, err := NewBuiltInToolExecutor(ToolExecutorConfig{})
	if err != nil {
		t.Fatalf("NewBuiltInToolExecutor: %v", err)
	}
	// We can't directly access shellTimeout, but we can verify it doesn't panic
	// and tools work with default timeout.
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestBuiltInToolExecutor_Definitions(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	defs := exec.Definitions(context.Background())
	if len(defs) != 12 {
		t.Fatalf("expected 12 tool definitions, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	expected := []string{"read_file", "write_file", "str_replace_file", "edit", "glob", "grep", "shell", "fetch_url", "list_directory", "TodoList", "dispatch_subagent", "read_video"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool definition: %s", name)
		}
	}
}

func TestBuiltInToolExecutor_IsReadOnly(t *testing.T) {
	t.Parallel()
	e := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	tests := []struct {
		name string
		want bool
	}{
		{"read_file", true},
		{"glob", true},
		{"grep", true},
		{"fetch_url", true},
		{"list_directory", true},
		{"TodoList", true},
		{"read_video", true},
		{"write_file", false},
		{"str_replace_file", false},
		{"edit", false},
		{"shell", false},
		{"web_search", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := e.IsReadOnly(tt.name); got != tt.want {
				t.Errorf("IsReadOnly(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "hello world" {
		t.Errorf("output = %q, want %q", result.Output, "hello world")
	}
}

func TestBuiltInToolExecutor_Execute_ReadVideo(t *testing.T) {
	skipNoFFmpeg(t)
	t.Parallel()

	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "test.mp4")
	createTestVideo(t, videoPath)

	exec := newTestExecutor(t, ToolExecutorConfig{
		ShellTimeout:   30 * time.Second,
		SandboxRoot:    tmp,
		VideoExtractor: NewVideoExtractor(),
	})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: fmt.Sprintf(`{"path":"%s","max_frames":2}`, videoPath),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "\"path\":\"") {
		t.Errorf("expected video metadata in output, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "data:image/png;base64,") {
		t.Errorf("expected base64 frame data in output, got: %s", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_ReadVideo_NotAvailable(t *testing.T) {
	t.Parallel()

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: `{"path":"video.mp4"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when video extractor is unavailable")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_SandboxEscape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{"path":"/etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for sandbox escape")
	}
	if !strings.Contains(result.Error, "sandbox") {
		t.Errorf("expected sandbox error, got: %s", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_NotFound(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{"path":"/nonexistent/file.txt"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing file")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_MissingPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_NonStringPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{"path":123}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected type error for non-string path")
	}
	if !strings.Contains(result.Error, "path") || !strings.Contains(result.Error, "string") {
		t.Errorf("expected path/string error, got: %s", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_TooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	largeFile := filepath.Join(tmp, "large.txt")
	if err := os.WriteFile(largeFile, make([]byte, maxFileReadSize+1), 0644); err != nil {
		t.Fatalf("create large file: %v", err)
	}

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, largeFile),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(result.Error, "max read size") {
		t.Errorf("error = %q, want containing 'max read size'", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_WriteFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "subdir", "out.txt")

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":"%s","content":"written data"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "written data" {
		t.Errorf("content = %q, want %q", string(data), "written data")
	}
}

func TestBuiltInToolExecutor_Execute_StrReplaceFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "replace.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"bar","new_string":"qux"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foo qux baz" {
		t.Errorf("content = %q, want %q", string(data), "foo qux baz")
	}
}

func TestBuiltInToolExecutor_Execute_StrReplaceFile_NotFound(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"/nonexistent","old_string":"a","new_string":"b"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing file")
	}
}

func TestBuiltInToolExecutor_Execute_StrReplaceFile_MissingOldString(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "replace.txt")
	_ = os.WriteFile(path, []byte("content"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"missing","new_string":"b"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing old_string")
	}
}

func TestBuiltInToolExecutor_Execute_Glob(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "b.go"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "a.txt"), []byte(""), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: fmt.Sprintf(`{"pattern":"%s"}`, filepath.Join(tmp, "*.go")),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	lines := strings.Split(result.Output, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %s", len(lines), result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Glob_MissingPattern(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing pattern")
	}
}

func TestBuiltInToolExecutor_Execute_ListDirectory(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte(""), 0644)
	_ = os.Mkdir(filepath.Join(tmp, "subdir"), 0755)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	if !strings.Contains(result.Output, "file a.go") {
		t.Errorf("output missing a.go: %s", result.Output)
	}
	if !strings.Contains(result.Output, "dir subdir") {
		t.Errorf("output missing subdir: %s", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_ListDirectory_MissingPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}
}

func TestBuiltInToolExecutor_Execute_Grep(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main\nfunc main() {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package test\nfunc foo() {}"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"package","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "package main") {
		t.Errorf("output missing expected match: %s", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Grep_NoMatches(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"nonexistent","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Grep_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Create a secret file outside the sandbox.
	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, "secret.txt")
	_ = os.WriteFile(secretPath, []byte("SECRET_PASSWORD=12345"), 0644)

	// Create a symlink inside the sandbox pointing to the secret.
	symlinkPath := filepath.Join(tmp, "link.txt")
	_ = os.Symlink(secretPath, symlinkPath)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"SECRET_PASSWORD","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET_PASSWORD") {
		t.Fatalf("grep should not follow symlinks; got output: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Shell(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo hello"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) != "hello" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(result.Output), "hello")
	}
}

func TestBuiltInToolExecutor_Execute_Shell_Timeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix process check")
	}

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 100 * time.Millisecond})

	// Embed a unique token in the command so we can look for a surviving shell
	// or sleep process after the timeout. A properly enforced timeout must
	// return well before the 5s sleep finishes and must not leave descendants.
	marker := fmt.Sprintf("kimi-shell-timeout-%d", time.Now().UnixNano())
	command := fmt.Sprintf("sleep 5 # %s", marker)

	start := time.Now()
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: fmt.Sprintf(`{"command":%q}`, command),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected timeout error to contain 'timed out', got: %q", result.Error)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("command took %v to return, want well under 1s for a 100ms timeout", elapsed)
	}

	// Best-effort: give the kernel a moment to reap the process group, then
	// confirm no shell or sleep process carrying our marker is still alive.
	time.Sleep(100 * time.Millisecond)
	if out, _ := osexec.Command("pgrep", "-f", marker).CombinedOutput(); len(out) > 0 {
		t.Fatalf("shell or sleep process still alive after timeout: %s", out)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_MissingCommand(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing command")
	}
}

func TestBuiltInToolExecutor_Execute_Shell_NonZeroExit(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo 'build failed'; exit 1"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Errorf("expected empty error for non-zero exit, got %q", result.Error)
	}
	if !strings.Contains(result.Output, "[exit status 1]") {
		t.Errorf("output = %q, want containing '[exit status 1]'", result.Output)
	}
	if !strings.Contains(result.Output, "build failed") {
		t.Errorf("output = %q, want containing 'build failed'", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_Disabled(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	exec.SetAllowShell(false)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo hello"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when shell is disabled")
	}
	if !strings.Contains(result.Error, "disabled") {
		t.Fatalf("expected disabled error, got: %q", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_CommandTooLong(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	longCmd := strings.Repeat("a", maxShellCommandLen+1)
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: fmt.Sprintf(`{"command":"%s"}`, longCmd),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for command exceeding max length")
	}
	if !strings.Contains(result.Error, "exceeds max length") {
		t.Fatalf("expected length error, got: %q", result.Error)
	}
}

func TestApprovalGate_NeverAutoApprove_Shell(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"shell", "read_file"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "shell"})
	if auto {
		t.Fatal("shell should never be auto-approved")
	}
	if decision != api.ApprovalNo {
		t.Fatalf("expected ApprovalNo, got %v", decision)
	}

	// read_file should still auto-approve.
	decision, auto = gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("read_file should be auto-approved")
	}
	if decision != api.ApprovalYes {
		t.Fatalf("expected ApprovalYes, got %v", decision)
	}
}

func TestApprovalGate_NeverAutoApprove_WriteFile(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"write_file", "str_replace_file"}, testIsReadOnly, nil)

	for _, name := range []string{"write_file", "str_replace_file"} {
		decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: name})
		if auto {
			t.Fatalf("%s should never be auto-approved", name)
		}
		if decision != api.ApprovalNo {
			t.Fatalf("expected ApprovalNo, got %v", decision)
		}
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BlocksLocalhost(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://localhost/test"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for localhost URL")
	}
	if !strings.Contains(result.Error, "blocked") {
		t.Errorf("expected blocked error, got: %s", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BlocksPrivateIP(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://192.168.1.1/test"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for private IP URL")
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_MissingURL(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing url")
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BlocksNonHTTP(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"file:///etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-http URL")
	}
}

func TestBuiltInToolExecutor_Execute_UnknownTool(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "unknown_tool",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for unknown tool")
	}
}

func TestBuiltInToolExecutor_Execute_InvalidArguments(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid arguments")
	}
}

func TestIsBlockedHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host    string
		blocked bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"100.64.0.0", true},
		{"100.127.255.255", true},
		{"fd12:3456::1", true},
		{"fc00::1", true},
		{"fc00::", true},
		{"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"fe80::1", true},
		{"fe80::", true},
		{"febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"::ffff:10.0.0.1", true},
		{"example.com", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			if got := isBlockedHost(tt.host); got != tt.blocked {
				t.Errorf("isBlockedHost(%q) = %v, want %v", tt.host, got, tt.blocked)
			}
		})
	}
}

func TestBuiltInToolExecutor_ValidatePath_Symlink_Escape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Create a file outside the sandbox
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	// Create a symlink inside the sandbox pointing outside
	symlinkPath := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, symlinkPath),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for symlink sandbox escape")
	}
	if !strings.Contains(result.Error, "sandbox") && !strings.Contains(result.Error, "blocked") {
		t.Errorf("expected sandbox/blocked error, got: %s", result.Error)
	}
}

func TestBuiltInToolExecutor_ValidatePath_Symlink_InsideSandbox(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	targetFile := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(targetFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	symlinkPath := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, symlinkPath),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "hello" {
		t.Errorf("output = %q, want %q", result.Output, "hello")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_BlocksHardlinkToOutside(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET_OUTSIDE_DATA"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, linkFile); err != nil {
		t.Skipf("hardlinks not supported in test environment: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, linkFile),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for hardlink to outside file")
	}
	if !strings.Contains(result.Error, "sandbox") && !strings.Contains(result.Error, "blocked") {
		t.Errorf("expected sandbox/blocked error, got: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET_OUTSIDE_DATA") {
		t.Errorf("should not read outside data; output: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_BlocksSymlinkParentEscape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET_PARENT_DATA"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	// A parent component inside the sandbox is a symlink to a directory outside.
	parentLink := filepath.Join(tmp, "parent")
	if err := os.Symlink(outside, parentLink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, filepath.Join(parentLink, "secret.txt")),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for symlink parent escape")
	}
	if !strings.Contains(result.Error, "sandbox") && !strings.Contains(result.Error, "blocked") {
		t.Errorf("expected sandbox/blocked error, got: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET_PARENT_DATA") {
		t.Errorf("should not read outside data; output: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_BlocksSymlinkParentTOCTOU(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}
	t.Parallel()

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET_TOCTOU_DATA"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	insideDir := filepath.Join(tmp, "parent")
	if err := os.MkdirAll(insideDir, 0755); err != nil {
		t.Fatalf("create inside dir: %v", err)
	}
	insideFile := filepath.Join(insideDir, "file.txt")
	if err := os.WriteFile(insideFile, []byte("inside"), 0644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	// Race a parent component between a real inside directory and a symlink to
	// an outside directory. Even if validation and opening race, os.Root must
	// never allow outside data to be read.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	swapped := make(chan struct{})
	go func() {
		defer close(swapped)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_ = os.RemoveAll(insideDir)
			_ = os.Symlink(outside, insideDir)
			_ = os.Remove(insideDir)
			_ = os.MkdirAll(insideDir, 0755)
			_ = os.WriteFile(insideFile, []byte("inside"), 0644)
		}
	}()

	var sawEscape bool
	for i := 0; i < 50; i++ {
		result, err := exec.Execute(context.Background(), api.ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      "read_file",
			Arguments: fmt.Sprintf(`{"path":"%s"}`, insideFile),
		})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if strings.Contains(result.Output, "SECRET_TOCTOU_DATA") {
			sawEscape = true
			break
		}
	}

	cancel()
	<-swapped

	if sawEscape {
		t.Fatal("read_file escaped sandbox via TOCTOU symlink parent swap")
	}
}

func TestNewSecureHTTPClient_BlocksRedirectToBlockedHost(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
	}))
	defer server.Close()

	client := netutil.SecureHTTPClient()
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error for redirect to blocked host")
	}
	if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "redirect") {
		t.Errorf("expected blocked/redirect error, got: %v", err)
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BlocksRedirectToLocalhost(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost/secret", http.StatusFound)
	}))
	defer server.Close()

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: fmt.Sprintf(`{"url":"%s"}`, server.URL),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for redirect to blocked host")
	}
	if !strings.Contains(result.Error, "blocked") && !strings.Contains(result.Error, "redirect") {
		t.Errorf("expected blocked/redirect error, got: %s", result.Error)
	}
}

// localRewriteTransport rewrites request hosts to a local test server so
// fetch_url tests can bypass the IsBlockedHost guard while still exercising
// the real HTTP path.
type localRewriteTransport struct {
	base http.RoundTripper
	host string
}

func (t *localRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Host = t.host
	return t.base.RoundTrip(req)
}

func testFetchClient(server *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &localRewriteTransport{
			base: http.DefaultTransport,
			host: server.Listener.Addr().String(),
		},
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_NonTextContentType(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("binarydata"))
	}))
	defer server.Close()

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, HTTPClient: testFetchClient(server)})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "binarydata") {
		t.Errorf("expected summary for non-text type, got raw bytes: %q", result.Output)
	}
	if !strings.Contains(result.Output, "image/png") {
		t.Errorf("expected summary to mention content type, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "binary or non-text data omitted") {
		t.Errorf("expected summary note, got: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_TextContentType_Delimited(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, HTTPClient: testFetchClient(server)})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "<html></html>") {
		t.Errorf("expected raw content, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "BEGIN UNTRUSTED EXTERNAL DATA") {
		t.Errorf("expected untrusted data delimiter, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "END UNTRUSTED EXTERNAL DATA") {
		t.Errorf("expected untrusted data delimiter, got: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BodyCapped(t *testing.T) {
	t.Parallel()
	largeBody := make([]byte, maxFetchBodySize+1024)
	for i := range largeBody {
		largeBody[i] = 'x'
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(largeBody)
	}))
	defer server.Close()

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, HTTPClient: testFetchClient(server)})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "truncated") {
		t.Errorf("expected truncation notice, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, fmt.Sprintf("%d", maxFetchBodySize)) {
		t.Errorf("expected max size in truncation notice, got: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_ValidatePath_BlocksExactEtc(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	_, err := exec.validatePath("/etc")
	if err == nil {
		t.Fatal("expected error for /etc")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}
}

func TestBuiltInToolExecutor_ReadFile_BlocksPrivateEtc(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sensitive POSIX paths not applicable on Windows")
	}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{"path":"/private/etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for sensitive path")
	}
	if !strings.Contains(result.Error, "blocked") {
		t.Fatalf("expected blocked error, got: %v", result.Error)
	}
}

func TestBuiltInToolExecutor_ValidatePath_BlocksSshDir(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	_, err := exec.validatePath("~/.ssh/id_rsa")
	if err == nil {
		t.Fatal("expected error for ~/.ssh/id_rsa")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}
}

func TestBuiltInToolExecutor_ValidatePath_BlocksProtectedDBPath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "sessions.db")

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp, ProtectedPaths: []string{dbPath}})

	_, err := exec.validatePath(dbPath)
	if err == nil {
		t.Fatal("expected error for protected DB path")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}

	// Also block the parent directory.
	_, err = exec.validatePath(filepath.Join(tmp, filepath.Dir(dbPath)))
	// Wait, dbPath is already inside tmp. The parent of dbPath is tmp.
	// Since sandboxRoot is tmp, accessing tmp itself is allowed by sandbox.
	// But protectedPaths should block it regardless.
	parentDir := filepath.Dir(dbPath)
	_, err = exec.validatePath(parentDir)
	if err == nil {
		t.Fatal("expected error for protected DB parent dir")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}
}

func TestAtomicWriteFile_NewFile_Mode0600(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "newfile.txt")

	if err := atomicWriteFile(target, []byte("hello")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Fatalf("expected mode 0600, got %04o", mode)
	}
}

func TestAtomicWriteFile_PreservesExistingMode(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "existing.txt")

	// Create existing file with mode 0644.
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	if err := atomicWriteFile(target, []byte("new")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0644 {
		t.Fatalf("expected mode 0644 to be preserved, got %04o", mode)
	}
}

func TestAtomicWriteFile_Atomic(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "atomic.txt")

	// Write a large payload so a non-atomic write would be observable.
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = 'x'
	}

	// Verify no temp file leaked.
	entries, _ := os.ReadDir(tmp)
	before := len(entries)

	if err := atomicWriteFile(target, payload); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	entries, _ = os.ReadDir(tmp)
	after := len(entries)
	if after != before+1 {
		t.Fatalf("expected 1 new file, got %d (before=%d, after=%d)", after-before, before, after)
	}

	// Verify target has the full content.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("expected %d bytes, got %d", len(payload), len(data))
	}
}

func TestCuratedEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/home/user")
	t.Setenv("USER", "user")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("ANTHROPIC_API_KEY", "anthro-secret")
	t.Setenv("KIMI_API_KEY", "kimi-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("GITHUB_TOKEN", "gh-secret")
	t.Setenv("MY_PASSWORD", "hunter2")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh")
	t.Setenv("SAFE_VAR", "visible")

	env := curatedEnv()
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}

	if m["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want /usr/bin", m["PATH"])
	}
	if m["HOME"] != "/home/user" {
		t.Errorf("HOME = %q, want /home/user", m["HOME"])
	}
	if m["USER"] != "user" {
		t.Errorf("USER = %q, want user", m["USER"])
	}
	if _, ok := m["OPENAI_API_KEY"]; ok {
		t.Error("OPENAI_API_KEY should be scrubbed")
	}
	if _, ok := m["ANTHROPIC_API_KEY"]; ok {
		t.Error("ANTHROPIC_API_KEY should be scrubbed")
	}
	if _, ok := m["KIMI_API_KEY"]; ok {
		t.Error("KIMI_API_KEY should be scrubbed")
	}
	if _, ok := m["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Error("AWS_SECRET_ACCESS_KEY should be scrubbed")
	}
	if _, ok := m["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN should be scrubbed")
	}
	if _, ok := m["MY_PASSWORD"]; ok {
		t.Error("MY_PASSWORD should be scrubbed")
	}
	if _, ok := m["SSH_AUTH_SOCK"]; ok {
		t.Error("SSH_AUTH_SOCK should be scrubbed")
	}
	if _, ok := m["SAFE_VAR"]; ok {
		t.Error("SAFE_VAR should be scrubbed because it is not in the allowlist")
	}
}

func TestBuiltInToolExecutor_Execute_WriteFile_NonStringContent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "existing.txt")
	if err := os.WriteFile(path, []byte("preserve me"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":"%s","content":12345}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected type error for non-string content")
	}
	if !strings.Contains(result.Error, "content") || !strings.Contains(result.Error, "string") {
		t.Errorf("expected content/string error, got: %s", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "preserve me" {
		t.Errorf("file was truncated; got %q, want %q", string(data), "preserve me")
	}
}

func TestBuiltInToolExecutor_Execute_Glob_RelativeAgainstSandbox(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "b.go"), []byte(""), 0644)

	// Use a relative pattern - should resolve against sandboxRoot, not cwd.
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{"pattern":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %s", len(lines), result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Glob_OutOfSandboxDropped(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.go"), []byte(""), 0644)

	// Pattern pointing outside sandbox should return empty results, not error.
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: fmt.Sprintf(`{"pattern":"%s"}`, filepath.Join(outside, "*.go")),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("expected no error for out-of-sandbox pattern, got: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_TruncationRuneSafe(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	tmp := t.TempDir()
	badFile := filepath.Join(tmp, "bad.txt")
	payload := make([]byte, maxShellOutputSize+2)
	for i := 0; i < maxShellOutputSize-1; i++ {
		payload[i] = 'x'
	}
	copy(payload[maxShellOutputSize-1:], []byte("あ")) // 3-byte rune
	if err := os.WriteFile(badFile, payload, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: fmt.Sprintf(`{"command":"cat %s"}`, badFile),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	out := result.Output
	idx := strings.Index(out, "\n... truncated")
	if idx == -1 {
		t.Fatalf("expected truncation notice, got: %q", out)
	}
	truncated := out[:idx]
	if !utf8.ValidString(truncated) {
		t.Errorf("truncated output is not valid UTF-8: %q", truncated)
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "test.txt")
	_ = os.WriteFile(path, []byte("hello"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
	if !strings.Contains(result.Error, "context canceled") {
		t.Errorf("expected context canceled error, got: %s", result.Error)
	}
}

func TestBuiltInToolExecutor_ValidatePath_SandboxRootAccepted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_, err := exec.validatePath(tmp)
	if err != nil {
		t.Fatalf("expected sandbox root to be accepted, got: %v", err)
	}
}

func TestBuiltInToolExecutor_ValidatePath_SentinelErrors(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	// Empty path -> ErrPathRequired
	_, err := exec.validatePath("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, ErrPathRequired) {
		t.Errorf("expected ErrPathRequired, got: %v", err)
	}

	// Blocked sensitive path -> ErrSandboxViolation
	_, err = exec.validatePath("/etc/passwd")
	if err == nil {
		t.Fatal("expected error for blocked path")
	}
	if !errors.Is(err, ErrSandboxViolation) {
		t.Errorf("expected ErrSandboxViolation, got: %v", err)
	}
	// On macOS /etc is a symlink to /private/etc; ensure the resolved path is not leaked.
	if strings.Contains(err.Error(), "/private/etc") {
		t.Errorf("error leaks resolved macOS path: %v", err)
	}
}

func TestBuiltInToolExecutor_execReadFile_ErrorWrapping(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	_, err := exec.execReadFile(context.Background(), readFileArgs{Path: "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSandboxViolation) {
		t.Errorf("expected errors.Is(err, ErrSandboxViolation), got: %v", err)
	}
}

func TestValidateFilePath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	insideFile := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(insideFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	symlinkInside := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(outsideFile, symlinkInside); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Inside sandbox returns the resolved absolute path.
	got, err := ValidateFilePath(insideFile, tmp, nil)
	if err != nil {
		t.Fatalf("expected inside-sandbox path to be valid, got error: %v", err)
	}
	want := insideFile
	if resolved, err := filepath.EvalSymlinks(insideFile); err == nil {
		want = resolved
	}
	if filepath.Clean(got) != want {
		t.Errorf("ValidateFilePath(%q, %q) = %q, want %q", insideFile, tmp, got, want)
	}

	// Outside sandbox is blocked.
	_, err = ValidateFilePath(outsideFile, tmp, nil)
	if err == nil {
		t.Fatal("expected error for outside-sandbox path")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("expected sandbox error, got: %v", err)
	}

	// Sensitive path is blocked.
	_, err = ValidateFilePath("/etc/passwd", "", nil)
	if err == nil {
		t.Fatal("expected error for sensitive path")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected blocked error, got: %v", err)
	}

	// Symlink escape is blocked.
	_, err = ValidateFilePath(symlinkInside, tmp, nil)
	if err == nil {
		t.Fatal("expected error for symlink sandbox escape")
	}
	if !strings.Contains(err.Error(), "sandbox") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected sandbox/blocked error, got: %v", err)
	}
}

func TestValidateFilePath_SymlinkToSecretTree_Blocked(t *testing.T) {
	homeDir := t.TempDir()
	realHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		t.Fatalf("resolve home dir: %v", err)
	}
	t.Setenv("HOME", realHome)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", realHome)
	}

	secretFile := filepath.Join(realHome, ".ssh", "id_rsa")
	if err := os.MkdirAll(filepath.Dir(secretFile), 0700); err != nil {
		t.Fatalf("create .ssh dir: %v", err)
	}
	if err := os.WriteFile(secretFile, []byte("secret"), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "link")
	if err := os.Symlink(secretFile, linkPath); err != nil {
		t.Skipf("symlinks not supported in test environment: %v", err)
	}

	_, err = ValidateFilePath(linkPath, "", nil)
	if err == nil {
		t.Fatal("expected error for symlink to secret tree")
	}
	if !errors.Is(err, ErrSandboxViolation) && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected blocked error, got: %v", err)
	}
}

func TestIsRootEscapeErr_RecognizesRootEscape(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root, err := os.OpenRoot(tmp)
	if err != nil {
		t.Fatalf("open sandbox root: %v", err)
	}
	defer root.Close()

	f, err := root.Open("../outside")
	if err == nil {
		_ = f.Close()
		t.Fatal("expected root-escape error")
	}
	if !isRootEscapeErr(err) {
		t.Errorf("isRootEscapeErr did not recognize root escape error: %v", err)
	}
	if isRootEscapeErr(errors.New("other error")) {
		t.Error("isRootEscapeErr should not match unrelated errors")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_Pagination(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "lines.txt")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name   string
		offset int
		nLines int
		want   string
	}{
		{"full file", 0, 0, content},
		{"first three", 0, 3, "line1\nline2\nline3"},
		{"middle two", 2, 2, "line2\nline3"},
		{"to end", 4, 0, "line4\nline5"},
		{"beyond end", 10, 1, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := exec.Execute(context.Background(), api.ToolCall{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: fmt.Sprintf(`{"path":"%s","line_offset":%d,"n_lines":%d}`, path, tc.offset, tc.nLines),
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}
			if result.Output != tc.want {
				t.Errorf("output = %q, want %q", result.Output, tc.want)
			}
		})
	}
}

func TestBuiltInToolExecutor_Execute_Edit_Unique(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"bar","new_string":"qux"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foo qux baz" {
		t.Errorf("content = %q, want %q", string(data), "foo qux baz")
	}
}

func TestBuiltInToolExecutor_Execute_Edit_MultipleRequiresReplaceAll(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("abc abc abc"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"abc","new_string":"xyz"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-unique old_string")
	}
	if !strings.Contains(result.Error, "occurs 3 times") {
		t.Errorf("expected occurrence count error, got: %q", result.Error)
	}

	result, err = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_2",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"abc","new_string":"xyz","replace_all":true}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "xyz xyz xyz" {
		t.Errorf("content = %q, want %q", string(data), "xyz xyz xyz")
	}
}

func TestBuiltInToolExecutor_Execute_StrReplaceFile_MultipleRequiresReplaceAll(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "replace.txt")
	if err := os.WriteFile(path, []byte("xx xx"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"xx","new_string":"yy"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-unique old_string")
	}

	result, err = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_2",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"xx","new_string":"yy","replace_all":true}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "yy yy" {
		t.Errorf("content = %q, want %q", string(data), "yy yy")
	}
}

func TestBuiltInToolExecutor_Execute_Shell_WorkingDirectory(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	if err := os.WriteFile(filepath.Join(tmp, "hello.txt"), []byte("world"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"cat hello.txt"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) != "world" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(result.Output), "world")
	}
}

func TestBuiltInToolExecutor_Execute_Shell_TimeoutOverride(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 5 * time.Second})
	marker := fmt.Sprintf("kimi-shell-timeout-override-%d", time.Now().UnixNano())

	start := time.Now()
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: fmt.Sprintf(`{"command":"sleep 3 # %s","timeout":1}`, marker),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected timeout error, got: %q", result.Error)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command took %v to return, want well under 2s", elapsed)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_CuratedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp, PassEnv: false})

	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/user")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("GITHUB_TOKEN", "gh-secret")
	t.Setenv("SAFE_VAR", "visible")

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo \"OPENAI=$OPENAI_API_KEY GITHUB=$GITHUB_TOKEN SAFE=$SAFE_VAR PATH=$PATH\""}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	out := strings.TrimSpace(result.Output)
	if strings.Contains(out, "sk-secret") || strings.Contains(out, "gh-secret") || strings.Contains(out, "SAFE=visible") {
		t.Errorf("sensitive/unknown variables leaked into shell env: %q", out)
	}
	if !strings.HasPrefix(out, "OPENAI= GITHUB= SAFE= PATH=/usr/bin:/bin") {
		t.Errorf("expected scrubbed env output, got: %q", out)
	}
}

func TestBuiltInToolExecutor_Execute_Shell_PassEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp, PassEnv: true})

	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("SAFE_VAR", "visible")

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo \"OPENAI=$OPENAI_API_KEY SAFE=$SAFE_VAR\""}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	out := strings.TrimSpace(result.Output)
	want := "OPENAI=sk-secret SAFE=visible"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestBuiltInToolExecutor_Definitions_WebSearchRegisteredWhenProviderSet(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{results: []api.WebSearchResult{{Title: "x", URL: "https://x", Snippet: "y"}}}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	defs := exec.Definitions(context.Background())
	if len(defs) != 13 {
		t.Fatalf("expected 13 tool definitions, got %d", len(defs))
	}
	found := false
	for _, d := range defs {
		if d.Name == "web_search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected web_search definition")
	}
	if !exec.IsReadOnly("web_search") {
		t.Error("web_search should be read-only")
	}
}

func TestBuiltInToolExecutor_Definitions_WebSearchNotRegisteredWhenProviderMissing(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	defs := exec.Definitions(context.Background())
	if len(defs) != 12 {
		t.Fatalf("expected 12 tool definitions, got %d", len(defs))
	}
	for _, d := range defs {
		if d.Name == "web_search" {
			t.Fatal("web_search should not be registered without a provider")
		}
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{
		results: []api.WebSearchResult{
			{Title: "Go", URL: "https://go.dev", Snippet: "The Go programming language", Date: "2024-01-01"},
			{Title: "Go Wiki", URL: "https://github.com/golang/go/wiki", Snippet: "Wiki", Content: "body text"},
		},
	}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"query":"golang","limit":2,"include_content":true}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Go") {
		t.Errorf("output missing title: %s", result.Output)
	}
	if !strings.Contains(result.Output, "https://go.dev") {
		t.Errorf("output missing URL: %s", result.Output)
	}
	if !strings.Contains(result.Output, "body text") {
		t.Errorf("output missing content: %s", result.Output)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 search call, got %d", len(fake.calls))
	}
	if fake.calls[0].Limit != 2 {
		t.Errorf("limit = %d, want 2", fake.calls[0].Limit)
	}
	if !fake.calls[0].IncludeContent {
		t.Error("expected IncludeContent=true")
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch_DefaultLimit(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{results: []api.WebSearchResult{{Title: "x", URL: "https://x", Snippet: "y"}}}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"query":"test"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(fake.calls) != 1 || fake.calls[0].Limit != 5 {
		t.Errorf("expected default limit 5, got %v", fake.calls)
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch_CapsLimit(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{results: []api.WebSearchResult{{Title: "x", URL: "https://x", Snippet: "y"}}}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	_, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"query":"test","limit":100}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].Limit != 20 {
		t.Errorf("expected capped limit 20, got %v", fake.calls)
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch_MissingProvider(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"query":"test"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when web search is not configured")
	}
	if !strings.Contains(result.Error, "not configured") {
		t.Errorf("expected not configured error, got: %q", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch_MissingQuery(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{results: []api.WebSearchResult{{Title: "x", URL: "https://x", Snippet: "y"}}}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing query")
	}
}

func TestBuiltInToolExecutor_Execute_WebSearch_ProviderError(t *testing.T) {
	t.Parallel()
	fake := &fakeWebSearcher{err: errors.New("provider down")}
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, WebSearcher: fake})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"query":"test"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error from provider")
	}
	if !strings.Contains(result.Error, "provider down") {
		t.Errorf("expected provider error, got: %q", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_TodoList_ReadEmpty(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "TodoList",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "Todo list is empty." {
		t.Errorf("output = %q, want %q", result.Output, "Todo list is empty.")
	}
}

func TestBuiltInToolExecutor_Execute_TodoList_WriteAndRead(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "TodoList",
		Arguments: `{"todos":[{"title":"Read tools.go","status":"done"},{"title":"Add TodoList tool","status":"in_progress"},{"title":"Add tests","status":"pending"}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Todo list updated.") {
		t.Errorf("expected update confirmation, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "[done] Read tools.go") {
		t.Errorf("expected done item, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "[in_progress] Add TodoList tool") {
		t.Errorf("expected in_progress item, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "[pending] Add tests") {
		t.Errorf("expected pending item, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "Mark tasks done immediately after finishing them") {
		t.Errorf("expected reminder, got: %q", result.Output)
	}

	// Read back the stored list.
	result, err = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_2",
		Name:      "TodoList",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "[in_progress] Add TodoList tool") {
		t.Errorf("read did not return stored list: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_TodoList_Clear(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	_, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "TodoList",
		Arguments: `{"todos":[{"title":"Task","status":"pending"}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_2",
		Name:      "TodoList",
		Arguments: `{"todos":[]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "Todo list cleared." {
		t.Errorf("output = %q, want %q", result.Output, "Todo list cleared.")
	}

	result, err = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_3",
		Name:      "TodoList",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "Todo list is empty." {
		t.Errorf("output = %q, want %q", result.Output, "Todo list is empty.")
	}
}

func TestBuiltInToolExecutor_Execute_TodoList_InvalidStatus(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "TodoList",
		Arguments: `{"todos":[{"title":"Task","status":"blocked"}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid status")
	}
	if !strings.Contains(result.Error, "invalid status") {
		t.Errorf("expected invalid status error, got: %q", result.Error)
	}
}

func TestBuiltInToolExecutor_Execute_TodoList_EmptyTitle(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "TodoList",
		Arguments: `{"todos":[{"title":"   ","status":"pending"}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for empty title")
	}
	if !strings.Contains(result.Error, "empty title") {
		t.Errorf("expected empty title error, got: %q", result.Error)
	}
}

type fakeSubagentRunner struct {
	req    *api.SubagentRequest
	result *api.SubagentResult
	err    error
}

func (f *fakeSubagentRunner) Run(ctx context.Context, req api.SubagentRequest) (*api.SubagentResult, error) {
	f.req = &req
	if f.result == nil {
		return &api.SubagentResult{Output: "done"}, nil
	}
	return f.result, f.err
}

func TestDispatchSubagent_Definition(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	defs := exec.Definitions(context.Background())

	if len(defs) != 12 {
		t.Fatalf("expected 12 tool definitions, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["dispatch_subagent"] {
		t.Fatal("missing dispatch_subagent tool definition")
	}
}

func TestDispatchSubagent_Execute_Explore(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	runner := &fakeSubagentRunner{result: &api.SubagentResult{Output: "exploration complete", Rounds: 2}}
	exec.SetSubagentRunner(runner)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "dispatch_subagent",
		Arguments: `{"type":"explore","prompt":"investigate the codebase","timeout_seconds":30,"max_rounds":5}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	var got api.SubagentResult
	if err := json.Unmarshal([]byte(result.Output), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.Output != "exploration complete" {
		t.Errorf("output = %q, want %q", got.Output, "exploration complete")
	}
	if got.Rounds != 2 {
		t.Errorf("rounds = %d, want 2", got.Rounds)
	}

	if runner.req == nil {
		t.Fatal("runner.Run was not called")
	}
	if runner.req.Type != api.SubagentExplore {
		t.Errorf("type = %q, want %q", runner.req.Type, api.SubagentExplore)
	}
	if runner.req.Prompt != "investigate the codebase" {
		t.Errorf("prompt = %q, want %q", runner.req.Prompt, "investigate the codebase")
	}
	if runner.req.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", runner.req.Timeout)
	}
	if runner.req.MaxRounds != 5 {
		t.Errorf("max_rounds = %d, want 5", runner.req.MaxRounds)
	}
}

func TestDispatchSubagent_MissingRunner(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "dispatch_subagent",
		Arguments: `{"type":"explore","prompt":"hi"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when subagent runner is not configured")
	}
	if !strings.Contains(result.Error, "not configured") {
		t.Errorf("expected not configured error, got: %q", result.Error)
	}
}

func TestDispatchSubagent_InvalidType(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	exec.SetSubagentRunner(&fakeSubagentRunner{})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "dispatch_subagent",
		Arguments: `{"type":"unknown","prompt":"hi"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid subagent type")
	}
	if !strings.Contains(result.Error, "invalid subagent type") {
		t.Errorf("expected invalid subagent type error, got: %q", result.Error)
	}
}

func TestDispatchSubagent_MissingPrompt(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	exec.SetSubagentRunner(&fakeSubagentRunner{})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "dispatch_subagent",
		Arguments: `{"type":"coder"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing prompt")
	}
	if !strings.Contains(result.Error, "prompt is required") {
		t.Errorf("expected prompt required error, got: %q", result.Error)
	}
}

func TestBuiltInToolExecutor_ToolCallHookBlocks(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	blocked := false
	exec.SetHookRunner(&fakeHookRunner{
		runFunc: func(ctx context.Context, data api.HookData) error {
			if data.Event == api.HookToolCall && data.ToolName == "write_file" {
				blocked = true
				return fmt.Errorf("blocked by policy")
			}
			return nil
		},
	})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: `{"path":"test.txt","content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !blocked {
		t.Fatal("expected tool_call hook to run")
	}
	if result.Error == "" {
		t.Fatal("expected tool to be blocked")
	}
	if !strings.Contains(result.Error, "blocked by policy") {
		t.Errorf("expected blocked by policy, got: %q", result.Error)
	}
}

type fakeHookRunner struct {
	runFunc func(ctx context.Context, data api.HookData) error
}

func (f *fakeHookRunner) Run(ctx context.Context, data api.HookData) error {
	if f.runFunc != nil {
		return f.runFunc(ctx, data)
	}
	return nil
}
