package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNewBuiltInToolExecutor_DefaultTimeout(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(0, "", nil)
	// We can't directly access shellTimeout, but we can verify it doesn't panic
	// and tools work with default timeout.
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestBuiltInToolExecutor_Definitions(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)
	defs := exec.Definitions()
	if len(defs) != 7 {
		t.Fatalf("expected 7 tool definitions, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	expected := []string{"read_file", "write_file", "str_replace_file", "glob", "grep", "shell", "fetch_url"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool definition: %s", name)
		}
	}
}

func TestBuiltInToolExecutor_IsReadOnly(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

	tests := []struct {
		name     string
		readonly bool
	}{
		{"read_file", true},
		{"glob", true},
		{"grep", true},
		{"fetch_url", true},
		{"write_file", false},
		{"str_replace_file", false},
		{"shell", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exec.IsReadOnly(tt.name); got != tt.readonly {
				t.Errorf("IsReadOnly(%q) = %v, want %v", tt.name, got, tt.readonly)
			}
		})
	}
}

func TestBuiltInToolExecutor_Execute_ReadFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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

func TestBuiltInToolExecutor_Execute_ReadFile_SandboxEscape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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

func TestBuiltInToolExecutor_Execute_WriteFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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

func TestBuiltInToolExecutor_Execute_Grep(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)
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

func TestBuiltInToolExecutor_Execute_Shell(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(100*time.Millisecond, "", nil)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"sleep 5"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected timeout error")
	}
}

func TestBuiltInToolExecutor_Execute_Shell_MissingCommand(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

	result, err := exec.Execute(context.Background(), api.ToolCall{
		ID:        "call_1",
		Name:      "shell",
		Arguments: `{"command":"echo 'build failed'; exit 1"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(result.Error, "exit code 1") {
		t.Errorf("error = %q, want containing 'exit code 1'", result.Error)
	}
	if !strings.Contains(result.Output, "build failed") {
		t.Errorf("output = %q, want containing 'build failed'", result.Output)
	}
}

func TestBuiltInToolExecutor_Execute_FetchURL_BlocksLocalhost(t *testing.T) {
	t.Parallel()
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)

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
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"fd12:3456::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"::ffff:10.0.0.1", true},
		{"example.com", false},
		{"8.8.8.8", false},
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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)

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
	exec := NewBuiltInToolExecutor(30*time.Second, tmp, nil)

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

func TestNewSecureHTTPClient_BlocksRedirectToBlockedHost(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
	}))
	defer server.Close()

	client := newSecureHTTPClient()
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

	exec := NewBuiltInToolExecutor(30*time.Second, "", nil)
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

func TestCuratedEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/home/user")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
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
	if m["SAFE_VAR"] != "visible" {
		t.Errorf("SAFE_VAR = %q, want visible", m["SAFE_VAR"])
	}
	if _, ok := m["OPENAI_API_KEY"]; ok {
		t.Error("OPENAI_API_KEY should be scrubbed")
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
}
