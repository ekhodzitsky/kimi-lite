package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	maxFileWriteSize   = 10 * 1024 * 1024 // 10 MB
	maxFileReadSize    = 10 * 1024 * 1024 // 10 MB
	maxShellOutputSize = 1024 * 1024      // 1 MB
)

// BuiltInToolExecutor executes the built-in tool set.
type BuiltInToolExecutor struct {
	shellTimeout   time.Duration
	readOnly       map[string]bool
	sandboxRoot    string
	httpClient     *http.Client
	protectedPaths []string
}

// NewBuiltInToolExecutor creates a new BuiltInToolExecutor.
// shellTimeout is applied to every shell command.
// protectedPaths are resolved and checked in validatePath; any path equal to
// or under a protected path is refused regardless of sandboxRoot.
func NewBuiltInToolExecutor(shellTimeout time.Duration, sandboxRoot string, httpClient *http.Client, protectedPaths ...string) *BuiltInToolExecutor {
	if shellTimeout <= 0 {
		shellTimeout = 30 * time.Second
	}
	if httpClient == nil {
		httpClient = netutil.SecureHTTPClient()
	}
	if sandboxRoot != "" {
		if resolved, err := filepath.EvalSymlinks(sandboxRoot); err == nil {
			sandboxRoot = resolved
		}
	}

	// Resolve and expand protected paths (files and their parent dirs).
	resolvedProtected := make([]string, 0, len(protectedPaths)*2)
	for _, p := range protectedPaths {
		p = expandTilde(p)
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			abs = resolved
		}
		resolvedProtected = append(resolvedProtected, abs)
		// Also protect the parent directory.
		parent := filepath.Dir(abs)
		if parent != abs {
			resolvedProtected = append(resolvedProtected, parent)
		}
	}

	return &BuiltInToolExecutor{
		shellTimeout:   shellTimeout,
		sandboxRoot:    sandboxRoot,
		httpClient:     httpClient,
		protectedPaths: resolvedProtected,
		readOnly: map[string]bool{
			"read_file": true,
			"glob":      true,
			"grep":      true,
			"fetch_url": true,
		},
	}
}

// expandTilde replaces a leading "~/" with the user's home directory.
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// isUnder reports whether target is equal to or under base.
// Both paths must be clean and absolute.
func isUnder(target, base string) bool {
	if target == base {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(base, sep) {
		base += sep
	}
	return strings.HasPrefix(target, base)
}

func (e *BuiltInToolExecutor) validatePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	path = expandTilde(path)
	cleaned := filepath.Clean(path)
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Check sensitive paths on the unresolved absolute path first.
	// This catches symlinks like /etc → /private/etc on macOS.
	sensitive := []string{"/etc", "/proc", "/sys", "/dev"}
	for _, s := range sensitive {
		if isUnder(absPath, s) {
			return "", fmt.Errorf("sandbox: access to %s is blocked", absPath)
		}
	}

	// Block user secret trees when no sandbox root is set (fallback case).
	if e.sandboxRoot == "" {
		secretTrees := []string{
			expandTilde("~/.ssh"),
			expandTilde("~/.aws"),
			expandTilde("~/.gnupg"),
		}
		for _, s := range secretTrees {
			if isUnder(absPath, s) {
				return "", fmt.Errorf("sandbox: access to %s is blocked", absPath)
			}
		}
	}

	// Block protected app paths on the unresolved path too.
	for _, protected := range e.protectedPaths {
		if isUnder(absPath, protected) {
			return "", fmt.Errorf("sandbox: access to %s is blocked", absPath)
		}
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve symlinks: %w", err)
		}
		// Walk up the directory tree to find the deepest existing
		// directory, resolve its symlinks, and append the rest.
		dir := absPath
		for {
			parent := filepath.Dir(dir)
			if parent == dir {
				return "", fmt.Errorf("resolve symlinks: %w", err)
			}
			resolvedParent, parentErr := filepath.EvalSymlinks(parent)
			if parentErr == nil {
				suffix := strings.TrimPrefix(absPath, parent)
				resolvedPath = filepath.Join(resolvedParent, strings.TrimPrefix(suffix, string(filepath.Separator)))
				break
			}
			dir = parent
		}
	}
	absPath = resolvedPath

	// Re-check protected paths on the resolved path as well.
	for _, protected := range e.protectedPaths {
		if isUnder(absPath, protected) {
			return "", fmt.Errorf("sandbox: access to %s is blocked", absPath)
		}
	}

	if e.sandboxRoot != "" {
		root := e.sandboxRoot
		if !strings.HasSuffix(root, string(filepath.Separator)) {
			root += string(filepath.Separator)
		}
		if !strings.HasPrefix(absPath, root) && absPath != e.sandboxRoot {
			return "", fmt.Errorf("path escapes sandbox")
		}
	}
	return absPath, nil
}

// Definitions returns all available tool definitions.
func (e *BuiltInToolExecutor) Definitions() []api.ToolDefinition {
	var defs []api.ToolDefinition

	addDef := func(name, desc string, params map[string]interface{}) {
		p, err := marshalParams(params)
		if err != nil {
			slog.Warn("failed to marshal tool parameters", "tool", name, "error", err)
			return
		}
		defs = append(defs, api.ToolDefinition{
			Name:        name,
			Description: desc,
			Parameters:  p,
		})
	}

	addDef("read_file", "Read the contents of a file at the given path.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to read.",
			},
		},
		"required": []string{"path"},
	})
	addDef("write_file", "Write content to a file at the given path, overwriting if it exists.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to write.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to write.",
			},
		},
		"required": []string{"path", "content"},
	})
	addDef("str_replace_file", "Replace every occurrence of old_string with new_string in a file.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file.",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The string to find.",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The string to replace with.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	})
	addDef("glob", "Find files matching a glob pattern.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The glob pattern (e.g. '*.go').",
			},
		},
		"required": []string{"pattern"},
	})
	addDef("grep", "Search for a pattern in file contents recursively.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The pattern to search for.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The directory or file to search in.",
			},
		},
		"required": []string{"pattern", "path"},
	})
	addDef("shell", "Execute a shell command with a configurable timeout.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute.",
			},
		},
		"required": []string{"command"},
	})
	addDef("fetch_url", "Fetch the content of a URL via HTTP GET.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch.",
			},
		},
		"required": []string{"url"},
	})

	return defs
}

func marshalParams(v map[string]interface{}) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal tool params: %w", err)
	}
	return b, nil
}

// Execute runs a tool call and returns the result.
func (e *BuiltInToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return api.ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  fmt.Sprintf("invalid arguments: %v", err),
		}, nil
	}

	result := api.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	switch call.Name {
	case "read_file":
		result.Output, result.Error = e.execReadFile(args)
	case "write_file":
		result.Output, result.Error = e.execWriteFile(args)
	case "str_replace_file":
		result.Output, result.Error = e.execStrReplaceFile(args)
	case "glob":
		result.Output, result.Error = e.execGlob(args)
	case "grep":
		result.Output, result.Error = e.execGrep(ctx, args)
	case "shell":
		result.Output, result.Error = e.execShell(ctx, args)
	case "fetch_url":
		result.Output, result.Error = e.execFetchURL(ctx, args)
	default:
		result.Error = fmt.Sprintf("unknown tool: %s", call.Name)
	}

	return result, nil
}

// IsReadOnly returns true if the tool does not modify state.
func (e *BuiltInToolExecutor) IsReadOnly(name string) bool {
	return e.readOnly[name]
}

func (e *BuiltInToolExecutor) execReadFile(args map[string]interface{}) (string, string) {
	path, _ := args["path"].(string)
	validPath, err := e.validatePath(path)
	if err != nil {
		return "", err.Error()
	}
	info, err := os.Stat(validPath)
	if err != nil {
		return "", fmt.Sprintf("read file: %v", err)
	}
	if info.Size() > maxFileReadSize {
		return "", fmt.Sprintf("file exceeds max read size of %d bytes", maxFileReadSize)
	}
	data, err := os.ReadFile(validPath)
	if err != nil {
		return "", fmt.Sprintf("read file: %v", err)
	}
	return string(data), ""
}

// atomicWriteFile writes data to a temporary file in the same directory as
// target, fsyncs it, then renames it over target. New files are created with
// mode 0600; existing files preserve their original mode.
func atomicWriteFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	mode := os.FileMode(0600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	f, err := os.CreateTemp(dir, ".kimi-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func (e *BuiltInToolExecutor) execWriteFile(args map[string]interface{}) (string, string) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if len(content) > maxFileWriteSize {
		return "", fmt.Sprintf("content exceeds max size of %d bytes", maxFileWriteSize)
	}
	validPath, err := e.validatePath(path)
	if err != nil {
		return "", err.Error()
	}
	if err := atomicWriteFile(validPath, []byte(content)); err != nil {
		return "", fmt.Sprintf("write file: %v", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), ""
}

func (e *BuiltInToolExecutor) execStrReplaceFile(args map[string]interface{}) (string, string) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	validPath, err := e.validatePath(path)
	if err != nil {
		return "", err.Error()
	}
	if oldStr == "" {
		return "", "old_string is required"
	}
	data, err := os.ReadFile(validPath)
	if err != nil {
		return "", fmt.Sprintf("read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return "", "old_string not found in file"
	}
	content = strings.ReplaceAll(content, oldStr, newStr)
	if len(content) > maxFileWriteSize {
		return "", fmt.Sprintf("result exceeds max size of %d bytes", maxFileWriteSize)
	}
	if err := atomicWriteFile(validPath, []byte(content)); err != nil {
		return "", fmt.Sprintf("write file: %v", err)
	}
	return fmt.Sprintf("replaced in %s", path), ""
}

func (e *BuiltInToolExecutor) execGlob(args map[string]interface{}) (string, string) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "", "pattern is required"
	}
	validPattern, err := e.validatePath(pattern)
	if err != nil {
		return "", err.Error()
	}
	matches, err := filepath.Glob(validPattern)
	if err != nil {
		return "", fmt.Sprintf("glob: %v", err)
	}
	return strings.Join(matches, "\n"), ""
}

func (e *BuiltInToolExecutor) execGrep(ctx context.Context, args map[string]interface{}) (string, string) {
	pattern, _ := args["pattern"].(string)
	path, _ := args["path"].(string)
	if pattern == "" {
		return "", "pattern is required"
	}
	validPath, err := e.validatePath(path)
	if err != nil {
		return "", err.Error()
	}
	// NOTE: GNU grep -r follows symlinks. There is no simple portable flag to disable this.
	// This is a known limitation of the current implementation.
	cmd := exec.CommandContext(ctx, "grep", "-r", "-n", "--exclude-dir=.git", pattern, validPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", "" // no matches
		}
		return "", fmt.Sprintf("grep: %v", err)
	}
	return string(output), ""
}

func (e *BuiltInToolExecutor) execShell(ctx context.Context, args map[string]interface{}) (string, string) {
	command, _ := args["command"].(string)
	if command == "" {
		return "", "command is required"
	}
	ctx, cancel := context.WithTimeout(ctx, e.shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = curatedEnv()
	if e.sandboxRoot != "" {
		cmd.Dir = e.sandboxRoot
	}
	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if len(outStr) > maxShellOutputSize {
		outStr = outStr[:maxShellOutputSize] + fmt.Sprintf("\n... truncated (%d bytes total)", len(output))
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
			return outStr, fmt.Sprintf("shell: exit code %d", exitErr.ExitCode())
		}
		return outStr, fmt.Sprintf("shell: %v", err)
	}
	return outStr, ""
}

// secretEnvPatterns lists substrings that indicate an environment variable
// likely contains sensitive material.
var secretEnvPatterns = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY",
	"AUTH", "BEARER", "JWT",
}

// curatedEnv returns a copy of the current process environment with
// likely-secret variables removed. It preserves PATH, HOME, and other
// safe variables so that common shell commands work.
func curatedEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "SSH") {
			continue
		}
		safe := true
		for _, p := range secretEnvPatterns {
			if strings.Contains(upper, p) {
				safe = false
				break
			}
		}
		if safe {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func (e *BuiltInToolExecutor) execFetchURL(ctx context.Context, args map[string]interface{}) (string, string) {
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return "", "url is required"
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Sprintf("invalid url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "only http and https URLs are allowed"
	}
	if netutil.IsBlockedHost(u.Hostname()) {
		return "", "URL host is blocked"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Sprintf("create request: %v", err)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Sprintf("fetch url: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", fmt.Sprintf("read body: %v", err)
	}
	return string(body), ""
}
