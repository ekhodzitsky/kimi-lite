package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeMetricsCollector records metric calls for tests.
type fakeMetricsCollector struct {
	counters  map[string]int
	latencies []string
	errors    []string
}

func newFakeMetricsCollector() *fakeMetricsCollector {
	return &fakeMetricsCollector{counters: make(map[string]int)}
}

func (f *fakeMetricsCollector) IncCounter(name string, tags ...string) {
	f.counters[name]++
}

func (f *fakeMetricsCollector) RecordLatency(name string, d time.Duration, tags ...string) {
	f.latencies = append(f.latencies, name)
}

func (f *fakeMetricsCollector) RecordError(name string) {
	f.errors = append(f.errors, name)
}

func TestBuiltInToolExecutor_SetMetricsCollector(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	mc := newFakeMetricsCollector()
	exec.SetMetricsCollector(mc)

	_, _ = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "unknown_tool",
		Arguments: `{}`,
	})

	if mc.counters["tool.called"] != 1 {
		t.Errorf("tool.called = %d, want 1", mc.counters["tool.called"])
	}
	if len(mc.latencies) != 1 || mc.latencies[0] != "tool.latency" {
		t.Errorf("unexpected latencies: %v", mc.latencies)
	}

	// Setting nil should fall back to the no-op collector without panic.
	exec.SetMetricsCollector(nil)
	_, _ = exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_2",
		Name:      "unknown_tool",
		Arguments: `{}`,
	})
}

func TestBuiltInToolExecutor_Close_Idempotent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	if err := exec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := exec.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestBuiltInToolExecutor_Execute_ToolResultHookError(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	var sawResult bool
	exec.SetHookRunner(&fakeHookRunner{
		runFunc: func(ctx context.Context, data api.HookData) error {
			if data.Event == api.HookToolResult {
				sawResult = true
				return fmt.Errorf("result hook failed")
			}
			return nil
		},
	})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "unknown_tool",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !sawResult {
		t.Fatal("expected tool_result hook to run")
	}
	if result.Error == "" {
		t.Fatal("expected unknown tool error")
	}
}

func TestReplaceInFile_NoRoot_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"bar","new_string":"qux","replace_all":true}`, path),
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

func TestReplaceInFile_NoRoot_MissingOldString(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"missing","new_string":"x"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing old_string")
	}
	if !strings.Contains(result.Error, "old_string not found") {
		t.Errorf("expected old_string not found, got: %q", result.Error)
	}
}

func TestReplaceInFile_NoRoot_ResultTooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	large := strings.Repeat("x", maxFileWriteSize+1)
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"a","new_string":"%s"}`, path, large),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for result too large")
	}
	if !strings.Contains(result.Error, "exceeds max size") {
		t.Errorf("expected max size error, got: %q", result.Error)
	}
}

func TestReplaceInFile_NoRoot_AtomicWriteFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chmod(tmp, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(tmp, 0755)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"a","new_string":"b"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when atomic write fails")
	}
}

func TestAtomicWriteFile_MkdirFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	// Create a file where the parent directory should be.
	blocking := filepath.Join(tmp, "parent")
	if err := os.WriteFile(blocking, []byte(""), 0644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	target := filepath.Join(blocking, "target.txt")
	if err := atomicWriteFile(target, []byte("data")); err == nil {
		t.Fatal("expected error when MkdirAll fails")
	}
}

func TestAtomicWriteFile_CreateTempFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	if err := os.Chmod(tmp, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(tmp, 0755)

	target := filepath.Join(tmp, "target.txt")
	if err := atomicWriteFile(target, []byte("data")); err == nil {
		t.Fatal("expected error when CreateTemp fails")
	}
}

func TestAtomicWriteFile_RenameFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// Make target an existing directory so rename fails.
	target := filepath.Join(tmp, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := atomicWriteFile(target, []byte("data"))
	if err == nil {
		t.Fatal("expected error when rename fails")
	}

	// No temp file should be left behind.
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestAtomicWriteFileRoot_RenameFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Make target an existing directory so rename fails.
	target := filepath.Join(tmp, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := exec.atomicWriteFileRoot("target", []byte("data"), false)
	if err == nil {
		t.Fatal("expected error when rename fails")
	}
}

func TestAtomicWriteFileRoot_HardlinkCheckFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	insideFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, insideFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	err := exec.atomicWriteFileRoot("link.txt", []byte("data"), false)
	if err == nil {
		t.Fatal("expected error for hardlink escape")
	}
	if !errors.Is(err, ErrSandboxViolation) && !strings.Contains(err.Error(), "hardlink") {
		t.Errorf("expected hardlink error, got: %v", err)
	}
}

func TestAtomicWriteFileRoot_CreateTempFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	if err := os.Chmod(tmp, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(tmp, 0755)

	err := exec.atomicWriteFileRoot("target.txt", []byte("data"), false)
	if err == nil {
		t.Fatal("expected error when create temp fails")
	}
}

func TestExecWriteFile_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "out.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":"%s","content":"hello"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
	if !strings.Contains(result.Error, "context canceled") {
		t.Errorf("expected context canceled, got: %q", result.Error)
	}
}

func TestExecWriteFile_ContentTooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "out.txt")

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":"%s","content":"%s"}`, path, strings.Repeat("x", maxFileWriteSize+1)),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for content too large")
	}
}

func TestExecWriteFile_PathValidationFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: `{"path":"/etc/passwd","content":"x"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}

func TestExecGlob_Root_AbsoluteOutsideDropped(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.go"), []byte(""), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: fmt.Sprintf(`{"pattern":"%s"}`, filepath.Join(outside, "*.go")),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got: %q", result.Output)
	}
}

func TestExecGlob_Root_InvalidPattern(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{"pattern":"["}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestExecGlob_NoRoot_InvalidPattern(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{"pattern":"["}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestReplaceInFile_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	_ = os.WriteFile(path, []byte("abc"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"abc","new_string":"xyz"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
}

func TestReplaceInFile_EmptyOldString(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	_ = os.WriteFile(path, []byte("abc"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"","new_string":"xyz"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for empty old_string")
	}
	if !strings.Contains(result.Error, "old_string is required") {
		t.Errorf("expected old_string required error, got: %q", result.Error)
	}
}

func TestReplaceInFileRoot_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"edit.txt","old_string":"bar","new_string":"qux","replace_all":true}`,
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

func TestReplaceInFileRoot_FileTooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, make([]byte, maxFileReadSize+1), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"edit.txt","old_string":"x","new_string":"y"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for file too large")
	}
}

func TestReplaceInFileRoot_ResultTooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	large := strings.Repeat("x", maxFileWriteSize+1)
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"edit.txt","old_string":"a","new_string":"%s"}`, large),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for result too large")
	}
}

func TestReplaceInFileRoot_MultipleWithoutReplaceAll(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("xx xx"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: `{"path":"edit.txt","old_string":"xx","new_string":"yy"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-unique old_string")
	}
}

func TestExecReadVideo_MaxFramesCap(t *testing.T) {
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
		Arguments: fmt.Sprintf(`{"path":"%s","max_frames":20}`, videoPath),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Count(result.Output, "data:image/png;base64,") > 10 {
		t.Errorf("expected max 10 frames, got more")
	}
}

func TestExecReadVideo_ExtractorError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	// Unavailable extractor with empty paths.
	exec.videoExtractor = &VideoExtractor{}

	videoPath := filepath.Join(tmp, "test.mp4")
	_ = os.WriteFile(videoPath, []byte("not a video"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, videoPath),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected extractor error")
	}
}

func TestExecReadVideo_InvalidPath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp, VideoExtractor: NewVideoExtractor()})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: `{"path":"/etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}

func TestExecListDirectory_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
}

func TestExecListDirectory_RootEscape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: `{"path":"../outside"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}

func TestExecListDirectory_ReadDirError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "file.txt")
	_ = os.WriteFile(path, []byte(""), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: `{"path":"file.txt"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected read dir error")
	}
}

func TestExecGrep_InvalidRegex(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"[","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid regex")
	}
}

func TestExecGrep_OutputLimit(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "file.txt")
	line := strings.Repeat("x", 100)
	var content strings.Builder
	for i := 0; i < 20000; i++ {
		content.WriteString(line)
		content.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(content.String()), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"x","path":"%s"}`, tmp),
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
}

func TestExecGrep_ScannerError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "huge.txt")
	// One line longer than the scanner max token size.
	_ = os.WriteFile(path, append(make([]byte, maxFileReadSize+1), '\n'), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"x","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Scanner error should cause the file to be skipped; no matches.
	if strings.Contains(result.Output, "huge.txt") {
		t.Errorf("expected scanner error to skip file, got: %q", result.Output)
	}
}

func TestExecGrep_NoRoot_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET_PASSWORD=123"), 0644)

	link := filepath.Join(tmp, "link.txt")
	_ = os.Symlink(filepath.Join(outside, "secret.txt"), link)

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
		t.Errorf("grep should not follow symlinks, got: %q", result.Output)
	}
}

func TestExecGrepRoot_OutputLimit(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "file.txt")
	line := strings.Repeat("x", 100)
	var content strings.Builder
	for i := 0; i < 20000; i++ {
		content.WriteString(line)
		content.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(content.String()), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"x","path":"."}`,
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
}

func TestExecGrepRoot_ScannerError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "huge.txt")
	_ = os.WriteFile(path, append(make([]byte, maxFileReadSize+1), '\n'), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"x","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "huge.txt") {
		t.Errorf("expected scanner error to skip file, got: %q", result.Output)
	}
}

func TestExtractLineRange_NegativeValues(t *testing.T) {
	t.Parallel()
	if _, err := extractLineRange([]byte("a\n"), -1, 0); err == nil {
		t.Error("expected error for negative offset")
	}
	if _, err := extractLineRange([]byte("a\n"), 0, -1); err == nil {
		t.Error("expected error for negative n_lines")
	}
}

func TestOpenFileNoFollow_Error(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
	}

	_, err := openFileNoFollow(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCheckFileHardlinkEscape_StatError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
	}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "file.txt")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	_ = f.Close()

	if err := checkFileHardlinkEscape(f); err == nil {
		t.Fatal("expected error for closed file")
	}
}

func TestKillProcessGroupPID_InvalidPID(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix process group semantics")
	}

	// A huge PID that does not exist should return an error.
	err := killProcessGroupPID(2147483647)
	if err == nil {
		t.Fatal("expected error for non-existent process group")
	}
}

func TestShellCommandContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := shellCommandContext(ctx, "echo hello")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

func TestRunCommandWithContext_ContextCancelled(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := shellCommandContext(ctx, "echo hello")
	_, err := runCommandWithContext(ctx, cmd, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

type bodyErrorTransport struct{}

func (bodyErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       errReadCloser{},
	}, nil
}

func TestBuiltInToolExecutor_Execute_FetchURL_BodyReadError(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, HTTPClient: &http.Client{Transport: bodyErrorTransport{}}})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected body read error")
	}
}
func TestExecGrep_NoRoot_SkipsGitDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	gitDir := filepath.Join(tmp, ".git")
	_ = os.Mkdir(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("SECRET=1"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"SECRET","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "config") {
		t.Errorf("grep should skip .git, got: %q", result.Output)
	}
}

func TestExecGrep_NoRoot_SkipsOversizedFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	largeFile := filepath.Join(tmp, "huge.txt")
	_ = os.WriteFile(largeFile, make([]byte, maxFileReadSize+1), 0644)
	normalFile := filepath.Join(tmp, "normal.txt")
	_ = os.WriteFile(normalFile, []byte("matchme"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"matchme","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "normal.txt") {
		t.Errorf("expected normal.txt match, got: %q", result.Output)
	}
}

func TestReplaceInFileRoot_MissingOldString(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "edit.txt")
	_ = os.WriteFile(path, []byte("content"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"edit.txt","old_string":"missing","new_string":"x"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing old_string")
	}
	if !strings.Contains(result.Error, "old_string not found") {
		t.Errorf("expected old_string not found, got: %q", result.Error)
	}
}

func TestReplaceInFileRoot_HardlinkEscape(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET"), 0644)
	insideFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, insideFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"link.txt","old_string":"SECRET","new_string":"x"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for hardlink escape")
	}
}

func TestExecFetchURL_DoError(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{
		ShellTimeout: 30 * time.Second,
		HTTPClient:   &http.Client{Transport: errorTransport{err: errors.New("network down")}},
	})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected fetch error")
	}
	if !strings.Contains(result.Error, "network down") {
		t.Errorf("expected network error, got: %q", result.Error)
	}
}

type errorTransport struct {
	err error
}

func (e errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}

func TestExecFetchURL_InvalidURL(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"ht!tp://[bad"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected invalid URL error")
	}
}

func TestIsBlockedHost_Boundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host    string
		blocked bool
	}{
		{"100.63.0.1", false},
		{"100.64.0.0", true},
		{"100.127.255.255", true},
		{"100.128.0.1", false},
		{"fc00::", true},
		{"fbff:ffff::", false},
		{"fe80::", true},
		{"fec0::", false},
		{"9.9.9.9", false},
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

func TestExecDispatchSubagent_AllTypes(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{"coder", "explore", "plan"} {
		t.Run(typ, func(t *testing.T) {
			t.Parallel()
			exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
			runner := &fakeSubagentRunner{}
			exec.SetSubagentRunner(runner)

			result, err := exec.Execute(context.Background(), api.ToolCall{
				ID:        "call_1",
				Name:      "dispatch_subagent",
				Arguments: fmt.Sprintf(`{"type":"%s","prompt":"hi"}`, typ),
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}
		})
	}
}

func TestExecDispatchSubagent_RunnerError(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	exec.SetSubagentRunner(&fakeSubagentRunner{
		result: &api.SubagentResult{},
		err:    errors.New("runner failed"),
	})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "dispatch_subagent",
		Arguments: `{"type":"coder","prompt":"hi"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected runner error")
	}
	if !strings.Contains(result.Error, "runner failed") {
		t.Errorf("expected runner error, got: %q", result.Error)
	}
}

func TestValidateFilePath_AbsError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix path semantics")
	}
	// A path longer than PATH_MAX can make filepath.Abs fail.
	longPath := "/tmp/" + strings.Repeat("a", 1024*1024)
	_, err := validateFilePath(longPath, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid absolute path")
	}
}

func TestValidateFilePath_ProtectedPathParent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	protected := filepath.Join(tmp, "secret.db")
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	// Accessing the parent directory of the protected file should be blocked.
	_, err := exec.validatePath(tmp)
	if err == nil {
		t.Fatal("expected error for protected path parent")
	}
}
func TestAtomicWriteFileRoot_ExistingFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "existing.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := exec.atomicWriteFileRoot("existing.txt", []byte("new"), false); err != nil {
		t.Fatalf("atomicWriteFileRoot: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", string(data), "new")
	}
}

func TestExecWriteFile_Root_AtomicWriteFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Create a file named "parent" so MkdirAll for "parent/file.txt" fails.
	_ = os.WriteFile(filepath.Join(tmp, "parent"), []byte(""), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: `{"path":"parent/file.txt","content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when atomic write fails")
	}
}

func TestExecGlob_Root_RelativePattern(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "b.go"), []byte(""), 0644)

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

func TestExecGrep_NoRoot_HardlinkEscapeWithProtectedPaths(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	_ = os.Mkdir(subDir, 0755)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{filepath.Join(subDir, "link.txt")}})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET_PASSWORD=123"), 0644)
	linkFile := filepath.Join(subDir, "link.txt")
	if err := os.Link(outsideFile, linkFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

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
		t.Errorf("grep should skip hardlink escape, got: %q", result.Output)
	}
}

func TestExecReadFile_RootEscapeError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Attempt to read a path that escapes the root. validateFilePath will
	// reject absolute paths outside the sandbox.
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: `{"path":"/etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}
func TestExecGrep_NoRoot_SkipsInaccessibleEntries(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	secretDir := filepath.Join(tmp, "secret")
	_ = os.Mkdir(secretDir, 0755)
	_ = os.WriteFile(filepath.Join(secretDir, "file.txt"), []byte("SECRET=1"), 0644)
	if err := os.Chmod(secretDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(secretDir, 0755)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"SECRET","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET") {
		t.Errorf("expected inaccessible entries to be skipped, got: %q", result.Output)
	}
}

func TestExecGrep_NoRoot_BrokenSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	_ = os.Symlink(filepath.Join(tmp, "missing.txt"), filepath.Join(tmp, "link.txt"))
	_ = os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("matchme"), 0644)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"matchme","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "real.txt") {
		t.Errorf("expected real.txt match, got: %q", result.Output)
	}
}

func TestExecGrepRoot_InaccessibleEntry(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	secretDir := filepath.Join(tmp, "secret")
	_ = os.Mkdir(secretDir, 0755)
	_ = os.WriteFile(filepath.Join(secretDir, "file.txt"), []byte("SECRET=1"), 0644)
	if err := os.Chmod(secretDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(secretDir, 0755)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"SECRET","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET") {
		t.Errorf("expected inaccessible entries to be skipped, got: %q", result.Output)
	}
}
func TestBuiltInToolExecutor_Execute_InvalidArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
	}{
		{"write_file", `{"path":123,"content":"x"}`},
		{"str_replace_file", `{"path":"x","old_string":1,"new_string":"y"}`},
		{"edit", `{"path":"x","old_string":"a","new_string":2}`},
		{"glob", `{"pattern":123}`},
		{"grep", `{"pattern":"x","path":123}`},
		{"shell", `{"command":123}`},
		{"fetch_url", `{"url":123}`},
		{"list_directory", `{"path":123}`},
		{"TodoList", `{"todos":"notarray"}`},
		{"dispatch_subagent", `{"type":123,"prompt":"hi"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
			result, err := exec.Execute(context.Background(), api.ToolCall{
				ID:        "call_1",
				Name:      tt.name,
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if result.Error == "" {
				t.Fatalf("expected error for invalid args in %s", tt.name)
			}
		})
	}
}

func TestExecReadVideo_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp, VideoExtractor: NewVideoExtractor()})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: `{"path":"video.mp4"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
}

func TestIsBlockedHost_NonIP(t *testing.T) {
	t.Parallel()
	if isBlockedHost("example.com") {
		t.Error("example.com should not be blocked")
	}
}

func TestBuiltInToolExecutor_New_ProtectedPathAbsError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix path semantics")
	}
	// A very long protected path may fail filepath.Abs.
	longPath := "/tmp/" + strings.Repeat("a", 1024*1024)
	_, err := NewBuiltInToolExecutor(ToolExecutorConfig{ProtectedPaths: []string{longPath}})
	if err != nil {
		t.Fatalf("NewBuiltInToolExecutor: %v", err)
	}
}
func TestNewBuiltInToolExecutor_OpenRootFails(t *testing.T) {
	t.Parallel()
	// A file cannot be opened as a sandbox root.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notadir")
	_ = os.WriteFile(path, []byte(""), 0644)

	_, err := NewBuiltInToolExecutor(ToolExecutorConfig{SandboxRoot: path})
	if err == nil {
		t.Fatal("expected error when sandbox root is not a directory")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_NegativeLineOffset(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "test.txt")
	_ = os.WriteFile(path, []byte("a\n"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s","line_offset":-1}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for negative line_offset")
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile_NegativeNLines(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	path := filepath.Join(tmp, "test.txt")
	_ = os.WriteFile(path, []byte("a\n"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"%s","n_lines":-1}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for negative n_lines")
	}
}

func TestExtractLineRange_StartIdxBeyondEnd(t *testing.T) {
	t.Parallel()
	out, err := extractLineRange([]byte("a\nb\n"), 5, 1)
	if err != nil {
		t.Fatalf("extractLineRange: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestExecReadVideo_MissingPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, VideoExtractor: NewVideoExtractor()})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "read_video",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}
}

func TestExecWriteFile_MissingPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: `{"content":"x"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}
}

func TestReplaceInFile_ValidatePathError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: `{"path":"/etc/passwd","old_string":"a","new_string":"b"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}

func TestReplaceInFileRoot_OpenDirectory(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_ = os.Mkdir(filepath.Join(tmp, "subdir"), 0755)
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"subdir","old_string":"a","new_string":"b"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when opening directory")
	}
}

func TestExecGlob_CancelledContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{"pattern":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected context cancellation error")
	}
}

func TestExecGrep_NoRoot_InvalidRegex(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"[","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid regex")
	}
}

func TestExecGrep_NoRoot_OutputLimit(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "file.txt")
	line := strings.Repeat("x", 100)
	var content strings.Builder
	for i := 0; i < 20000; i++ {
		content.WriteString(line)
		content.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(content.String()), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"x","path":"%s"}`, tmp),
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
}

func TestExecGrep_NoRoot_ScannerError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "huge.txt")
	_ = os.WriteFile(path, append(make([]byte, maxFileReadSize+1), '\n'), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"x","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "huge.txt") {
		t.Errorf("expected scanner error to skip file, got: %q", result.Output)
	}
}

func TestExecGrep_NoRoot_HardlinkEscape(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET_PASSWORD=123"), 0644)

	linkFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, linkFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{outsideFile}})
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
		t.Errorf("grep should skip hardlink escape, got: %q", result.Output)
	}
}

func TestExecGlob_NoRoot_Output(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a"), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package b"), 0644)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
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
	if !strings.Contains(result.Output, "a.go") || !strings.Contains(result.Output, "b.go") {
		t.Errorf("expected both .go files, got: %q", result.Output)
	}
}
func TestValidateFilePath_SymlinkToSensitivePath_Blocked(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	link := filepath.Join(tmp, "link")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	_, err := ValidateFilePath(link, "", nil)
	if err == nil {
		t.Fatal("expected error for symlink to sensitive path")
	}
	if !errors.Is(err, ErrSandboxViolation) && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected blocked error, got: %v", err)
	}
}

func TestMarshalParams_Error(t *testing.T) {
	t.Parallel()
	_, err := marshalParams(map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatal("expected error for unsupported value")
	}
}

func TestLimitedWriter_DiscardsAfterLimit(t *testing.T) {
	t.Parallel()
	w := newLimitedWriter(5)
	if n, err := w.Write([]byte("hello world")); n != 11 || err != nil {
		t.Fatalf("Write = (%d, %v), want (11, nil)", n, err)
	}
	if w.buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", w.buf.String(), "hello")
	}
	if w.written != 11 {
		t.Errorf("written = %d, want 11", w.written)
	}
}
func TestExecReadFile_NoRoot_HardlinkEscapeWithProtectedPaths(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	// Protected path is a different file, so validatePath allows link.txt.
	protected := filepath.Join(tmp, "other.txt")
	_ = os.WriteFile(protected, []byte(""), 0644)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET"), 0644)
	linkFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, linkFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
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
		t.Fatal("expected hardlink escape error")
	}
}

func TestReplaceInFile_NoRoot_HardlinkEscapeWithProtectedPaths(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	protected := filepath.Join(tmp, "other.txt")
	_ = os.WriteFile(protected, []byte(""), 0644)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET"), 0644)
	linkFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, linkFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"SECRET","new_string":"x"}`, linkFile),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected hardlink escape error")
	}
}

func TestExecGlob_NoRoot_SymlinkEscapeDropped(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.go"), []byte(""), 0644)

	linkDir := filepath.Join(tmp, "link")
	_ = os.Symlink(outside, linkDir)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "glob",
		Arguments: `{"pattern":"link/*.go"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected out-of-sandbox matches to be dropped, got: %q", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_InvalidArgs_ReadFileReadVideoWebSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
	}{
		{"read_file", `{"path":123}`},
		{"read_video", `{"path":123}`},
		{"web_search", `{"query":123}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
			result, err := exec.Execute(context.Background(), api.ToolCall{
				ID:        "call_1",
				Name:      tt.name,
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if result.Error == "" {
				t.Fatalf("expected error for invalid args in %s", tt.name)
			}
		})
	}
}

func TestAtomicWriteFile_ExistingFilePreservesMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	target := filepath.Join(tmp, "existing.txt")
	if err := os.WriteFile(target, []byte("old"), 0640); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := atomicWriteFile(target, []byte("new")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0640 {
		t.Errorf("mode = %o, want 0640", info.Mode().Perm())
	}
}

func TestAtomicWriteFileRoot_MkdirFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	// Create a file where a directory is expected so MkdirAll fails.
	_ = os.WriteFile(filepath.Join(tmp, "parent"), []byte(""), 0644)

	err := exec.atomicWriteFileRoot("parent/file.txt", []byte("data"), false)
	if err == nil {
		t.Fatal("expected error when MkdirAll fails")
	}
}

func TestAtomicWriteFileRoot_ExistingFileHardlinkOutside(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET"), 0644)
	insideFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, insideFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	err := exec.atomicWriteFileRoot("link.txt", []byte("data"), false)
	if err == nil {
		t.Fatal("expected error for hardlink escape")
	}
}

func TestReplaceInFile_NoRoot_FileTooLarge(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	_ = os.WriteFile(path, make([]byte, maxFileReadSize+1), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"x","new_string":"y"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for file too large")
	}
}

func TestReplaceInFileRoot_RootEscapeError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	err := exec.replaceInFileRoot("..", "old", "new", false, "edit")
	if err == nil {
		t.Fatal("expected root escape error")
	}
	if !errors.Is(err, ErrSandboxViolation) && !strings.Contains(err.Error(), "escapes sandbox") {
		t.Errorf("expected sandbox error, got: %v", err)
	}
}

func TestExecGrepRoot_SkipsGitDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	gitDir := filepath.Join(tmp, ".git")
	_ = os.Mkdir(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("SECRET=1"), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("matchme"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"matchme","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "real.txt") {
		t.Errorf("expected real.txt match, got: %q", result.Output)
	}
	if strings.Contains(result.Output, "config") {
		t.Errorf("grep should skip .git, got: %q", result.Output)
	}
}

func TestExecGrepRoot_SkipsSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET_PASSWORD=123"), 0644)
	_ = os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(tmp, "link.txt"))
	_ = os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("matchme"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"matchme","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "real.txt") {
		t.Errorf("expected real.txt match, got: %q", result.Output)
	}
	if strings.Contains(result.Output, "SECRET_PASSWORD") {
		t.Errorf("grep should skip symlinks, got: %q", result.Output)
	}
}

func TestExecGrepRoot_SkipsOversizedFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_ = os.WriteFile(filepath.Join(tmp, "huge.txt"), make([]byte, maxFileReadSize+1), 0644)
	_ = os.WriteFile(filepath.Join(tmp, "normal.txt"), []byte("matchme"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"matchme","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "normal.txt") {
		t.Errorf("expected normal.txt match, got: %q", result.Output)
	}
}

func TestExecGrepRoot_HardlinkEscape(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("SECRET_PASSWORD=123"), 0644)
	insideFile := filepath.Join(tmp, "link.txt")
	if err := os.Link(outsideFile, insideFile); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"SECRET_PASSWORD","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "SECRET_PASSWORD") {
		t.Errorf("grep should skip hardlink escape, got: %q", result.Output)
	}
}

func TestRunCommandWithContext_StartError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "/nonexistent-binary-for-test")
	_, err := runCommandWithContext(ctx, cmd, nil)
	if err == nil {
		t.Fatal("expected error when command fails to start")
	}
}

func TestExecShell_TimeoutCapped(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	exec.SetAllowShell(true)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo hello","timeout":9999}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected hello output, got: %q", result.Output)
	}
}

func TestExecShell_InvalidCommandStartError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})
	exec.SetAllowShell(true)

	// Use a shell built-in syntax error that still starts the shell but exits
	// non-zero, covering the exit-error branch rather than start error.
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"exit 42"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "[exit status 42]") {
		t.Errorf("expected exit status 42, got: %q", result.Output)
	}
}

func TestExecGrep_NoRoot_RelativePath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("matchme"), 0644)

	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"matchme","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "a.txt") {
		t.Errorf("expected a.txt match, got: %q", result.Output)
	}
}

type staticTransport struct {
	status      int
	contentType string
	body        []byte
}

func (s staticTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Header:     http.Header{"Content-Type": []string{s.contentType}},
		Body:       io.NopCloser(bytes.NewReader(s.body)),
	}, nil
}

func TestExecFetchURL_BinaryContent(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{
		ShellTimeout: 30 * time.Second,
		HTTPClient:   &http.Client{Transport: staticTransport{status: http.StatusOK, contentType: "image/png", body: []byte("PNG")}},
	})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com/image.png"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "binary or non-text data omitted") {
		t.Errorf("expected binary content notice, got: %q", result.Output)
	}
}

func TestExecFetchURL_TruncationNotice(t *testing.T) {
	t.Parallel()
	body := make([]byte, maxFetchBodySize)
	for i := range body {
		body[i] = 'x'
	}
	exec := newTestExecutor(t, ToolExecutorConfig{
		ShellTimeout: 30 * time.Second,
		HTTPClient:   &http.Client{Transport: staticTransport{status: http.StatusOK, contentType: "text/plain", body: body}},
	})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com/large.txt"}`,
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
}

func TestExecWriteFile_NoRoot_AtomicWriteFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "out.txt")

	if err := os.Chmod(tmp, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(tmp, 0755)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":"%s","content":"hello"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error when atomic write fails")
	}
}

func TestReplaceInFile_MissingPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: `{"old_string":"a","new_string":"b"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}
}

func TestReplaceInFile_NoRoot_MultipleWithoutReplaceAll(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "edit.txt")
	_ = os.WriteFile(path, []byte("xx xx"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "edit",
		Arguments: fmt.Sprintf(`{"path":"%s","old_string":"xx","new_string":"yy"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-unique old_string")
	}
}

func TestExecGrep_EmptyPattern(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: fmt.Sprintf(`{"pattern":"","path":"%s"}`, tmp),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for empty pattern")
	}
}

func TestExecGrep_EmptyPath(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"x","path":""}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for empty path")
	}
}

func TestExecGrep_PathValidationFails(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"x","path":"/etc/passwd"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected sandbox error")
	}
}

func TestExecListDirectory_NoRoot_ReadDirError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second})
	path := filepath.Join(tmp, "file.txt")
	_ = os.WriteFile(path, []byte(""), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "list_directory",
		Arguments: fmt.Sprintf(`{"path":"%s"}`, path),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected read dir error")
	}
}

func TestExecReadFile_RootEscapeError_Direct(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_, err := exec.execReadFile(context.Background(), readFileArgs{Path: ".."})
	if err == nil {
		t.Fatal("expected root escape error")
	}
	if !errors.Is(err, ErrSandboxViolation) && !strings.Contains(err.Error(), "escapes sandbox") {
		t.Errorf("expected sandbox error, got: %v", err)
	}
}

func TestExecFetchURL_Non2xxStatus(t *testing.T) {
	t.Parallel()
	exec := newTestExecutor(t, ToolExecutorConfig{
		ShellTimeout: 30 * time.Second,
		HTTPClient:   &http.Client{Transport: staticTransport{status: http.StatusNotFound, contentType: "text/plain", body: []byte("not found")}},
	})
	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "fetch_url",
		Arguments: `{"url":"http://example.com/missing"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected HTTP error")
	}
}

func TestLimitedWriter_SecondWriteAtLimit(t *testing.T) {
	t.Parallel()
	w := newLimitedWriter(5)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// A second write when the buffer is already at the limit should hit the
	// early-return branch and not extend the buffer.
	if _, err := w.Write([]byte("world")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if w.buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", w.buf.String(), "hello")
	}
	if w.written != 10 {
		t.Errorf("written = %d, want 10", w.written)
	}
}

func TestExecReadFile_NoRoot_HardlinkCheckDirect(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	protected := filepath.Join(outside, "x.txt")
	_ = os.WriteFile(protected, []byte(""), 0644)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	f1 := filepath.Join(tmp, "a.txt")
	f2 := filepath.Join(tmp, "b.txt")
	_ = os.WriteFile(f1, []byte("data"), 0644)
	if err := os.Link(f1, f2); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	_, err := exec.execReadFile(context.Background(), readFileArgs{Path: f2})
	if err == nil {
		t.Fatal("expected hardlink escape error")
	}
}

func TestReplaceInFileNoRoot_HardlinkCheckDirect(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	protected := filepath.Join(outside, "x.txt")
	_ = os.WriteFile(protected, []byte(""), 0644)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	f1 := filepath.Join(tmp, "a.txt")
	f2 := filepath.Join(tmp, "b.txt")
	_ = os.WriteFile(f1, []byte("data"), 0644)
	if err := os.Link(f1, f2); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	err := exec.replaceInFileNoRoot(f2, "data", "x", false, "edit")
	if err == nil {
		t.Fatal("expected hardlink escape error")
	}
}

func TestExecGrep_NoRoot_HardlinkCheckDirect(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on Windows")
	}

	tmp := t.TempDir()
	outside := t.TempDir()
	protected := filepath.Join(outside, "x.txt")
	_ = os.WriteFile(protected, []byte(""), 0644)
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, ProtectedPaths: []string{protected}})

	f1 := filepath.Join(tmp, "a.txt")
	f2 := filepath.Join(tmp, "b.txt")
	_ = os.WriteFile(f1, []byte("matchme"), 0644)
	if err := os.Link(f1, f2); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	re, err := compileRegexpWithContext(context.Background(), "matchme")
	if err != nil {
		t.Fatalf("compile regexp: %v", err)
	}

	_, err = exec.execGrep(context.Background(), grepArgs{Pattern: "matchme", Path: tmp})
	if err != nil {
		t.Fatalf("execGrep: %v", err)
	}
	// The hardlink file should be skipped, so no panic or leaked match.
	_ = re
}

func TestExtractLineRange_NLinesBeyondEnd(t *testing.T) {
	t.Parallel()
	out, err := extractLineRange([]byte("a\nb\n"), 1, 10)
	if err != nil {
		t.Fatalf("extractLineRange: %v", err)
	}
	if out != "a\nb" {
		t.Errorf("output = %q, want %q", out, "a\nb")
	}
}

func TestExecGrepRoot_BrokenSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}

	tmp := t.TempDir()
	exec := newTestExecutor(t, ToolExecutorConfig{ShellTimeout: 30 * time.Second, SandboxRoot: tmp})

	_ = os.Symlink(filepath.Join(tmp, "missing.txt"), filepath.Join(tmp, "link.txt"))
	_ = os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("matchme"), 0644)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "grep",
		Arguments: `{"pattern":"matchme","path":"."}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "real.txt") {
		t.Errorf("expected real.txt match, got: %q", result.Output)
	}
}
