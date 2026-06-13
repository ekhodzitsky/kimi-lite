package core

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	maxFileWriteSize   = 10 * 1024 * 1024 // 10 MB
	maxFileReadSize    = 10 * 1024 * 1024 // 10 MB
	maxShellOutputSize = 1024 * 1024      // 1 MB
	maxShellCommandLen = 4096             // 4 KB
	maxFetchBodySize   = 2 * 1024 * 1024  // 2 MB
)

var (
	// ErrSandboxViolation is returned when a path is blocked by the sandbox.
	ErrSandboxViolation = errors.New("sandbox violation")
	// ErrPathRequired is returned when a path argument is empty.
	ErrPathRequired = errors.New("path is required")
	// errOutputLimitReached is returned by grep walkers when the output cap
	// is exceeded.
	errOutputLimitReached = errors.New("output limit reached")
)

// BuiltInToolExecutor executes the built-in tool set.
type BuiltInToolExecutor struct {
	shellTimeout   time.Duration
	readOnly       map[string]bool
	sandboxRoot    string
	httpClient     *http.Client
	protectedPaths []string
	allowShell     bool
	root           *os.Root
}

// ToolExecutorConfig holds configuration for NewBuiltInToolExecutor.
type ToolExecutorConfig struct {
	ShellTimeout   time.Duration
	SandboxRoot    string
	HTTPClient     *http.Client
	ProtectedPaths []string
}

// NewBuiltInToolExecutor creates a new BuiltInToolExecutor.
// ShellTimeout is applied to every shell command.
// ProtectedPaths are resolved and checked in validatePath; any path equal to
// or under a protected path is refused regardless of SandboxRoot.
// If SandboxRoot is non-empty, an error is returned when the root cannot be
// opened (e.g. permissions or an unsupported platform).
func NewBuiltInToolExecutor(cfg ToolExecutorConfig) (*BuiltInToolExecutor, error) {
	shellTimeout := cfg.ShellTimeout
	if shellTimeout <= 0 {
		shellTimeout = 30 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = netutil.SecureHTTPClient()
	}
	sandboxRoot := cfg.SandboxRoot
	if sandboxRoot != "" {
		if resolved, err := filepath.EvalSymlinks(sandboxRoot); err == nil {
			sandboxRoot = resolved
		}
	}

	var root *os.Root
	if sandboxRoot != "" {
		r, err := os.OpenRoot(sandboxRoot)
		if err != nil {
			return nil, fmt.Errorf("open sandbox root %q: %w", sandboxRoot, err)
		}
		root = r
	}

	// Resolve and expand protected paths (files and their parent dirs).
	resolvedProtected := make([]string, 0, len(cfg.ProtectedPaths)*2)
	for _, p := range cfg.ProtectedPaths {
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
		allowShell:     true,
		root:           root,
		readOnly: map[string]bool{
			"read_file":      true,
			"glob":           true,
			"grep":           true,
			"fetch_url":      true,
			"list_directory": true,
		},
	}, nil
}

// SetAllowShell controls whether the shell tool is enabled.
func (e *BuiltInToolExecutor) SetAllowShell(v bool) {
	e.allowShell = v
}

// IsReadOnly reports whether the named built-in tool is read-only.
func (e *BuiltInToolExecutor) IsReadOnly(name string) bool {
	return e.readOnly[name]
}

// Close closes the sandbox root, if any.
func (e *BuiltInToolExecutor) Close() error {
	if e.root != nil {
		if err := e.root.Close(); err != nil {
			return fmt.Errorf("close sandbox root: %w", err)
		}
	}
	return nil
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

// secretTreePaths returns the secret directories that are blocked when no
// sandbox root is configured.
func secretTreePaths() []string {
	return []string{
		expandTilde("~/.ssh"),
		expandTilde("~/.aws"),
		expandTilde("~/.gnupg"),
	}
}

// ValidateFilePath validates a file path for read or write access and returns
// the resolved absolute path. It mirrors the checks performed by the built-in
// tool executor: expands tilde, resolves symlinks, blocks sensitive system
// paths and secret trees (only when sandboxRoot is empty), checks protected
// paths, and optionally enforces sandboxRoot containment.
func ValidateFilePath(path string, sandboxRoot string, protectedPaths []string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: path is required", ErrPathRequired)
	}
	originalPath := path
	if sandboxRoot != "" {
		if resolved, err := filepath.EvalSymlinks(sandboxRoot); err == nil {
			sandboxRoot = resolved
		}
	}
	path = expandTilde(path)
	cleaned := filepath.Clean(path)
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Check sensitive paths on the unresolved absolute path first.
	// This catches symlinks like /etc → /private/etc on macOS.
	// Note: these are POSIX-specific paths; they are a silent no-op on Windows.
	sensitive := []string{"/etc", "/private/etc", "/proc", "/sys", "/dev"}
	for _, s := range sensitive {
		if isUnder(absPath, s) {
			return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
		}
	}

	if sandboxRoot == "" {
		for _, s := range secretTreePaths() {
			if isUnder(absPath, s) {
				return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
			}
		}
	}

	for _, protected := range protectedPaths {
		if isUnder(absPath, protected) {
			return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
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

	// Re-check sensitive and protected paths on the resolved path.
	for _, s := range sensitive {
		if isUnder(absPath, s) {
			return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
		}
	}
	for _, protected := range protectedPaths {
		if isUnder(absPath, protected) {
			return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
		}
	}
	// Re-check secret trees on the resolved path to block symlinks that point
	// into sensitive directories (e.g. /tmp/link -> ~/.ssh/id_rsa).
	if sandboxRoot == "" {
		for _, s := range secretTreePaths() {
			if isUnder(absPath, s) {
				return "", fmt.Errorf("%w: access to %q is blocked", ErrSandboxViolation, originalPath)
			}
		}
	}

	if sandboxRoot != "" {
		rel, err := filepath.Rel(sandboxRoot, absPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: path escapes sandbox", ErrSandboxViolation)
		}
	}
	return absPath, nil
}

func (e *BuiltInToolExecutor) validatePath(path string) (string, error) {
	absPath, err := ValidateFilePath(path, e.sandboxRoot, e.protectedPaths)
	if err != nil {
		return "", err
	}
	if e.root == nil {
		return absPath, nil
	}
	// Convert the validated absolute path to a root-relative path for os.Root.
	rel, err := filepath.Rel(e.sandboxRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("%w: path escapes sandbox", ErrSandboxViolation)
	}
	return rel, nil
}

type readFileArgs struct {
	Path string `json:"path"`
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type strReplaceArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type globArgs struct {
	Pattern string `json:"pattern"`
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

type shellArgs struct {
	Command string `json:"command"`
}

type fetchURLArgs struct {
	URL string `json:"url"`
}

type listDirArgs struct {
	Path string `json:"path"`
}

func decodeArgs[T any](raw string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, fmt.Errorf("invalid arguments: %w", err)
	}
	return v, nil
}

// Definitions returns all available tool definitions.
func (e *BuiltInToolExecutor) Definitions(_ context.Context) []api.ToolDefinition {
	defs := make([]api.ToolDefinition, 0, 8)

	addDef := func(name, desc string, params map[string]any) {
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

	addDef("read_file", "Read the contents of a file at the given path.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file to read.",
			},
		},
		"required": []string{"path"},
	})
	addDef("write_file", "Write content to a file at the given path, overwriting if it exists.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file to write.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write.",
			},
		},
		"required": []string{"path", "content"},
	})
	addDef("str_replace_file", "Replace every occurrence of old_string with new_string in a file.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The string to find.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The string to replace with.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	})
	addDef("glob", "Find files matching a glob pattern.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The glob pattern (e.g. '*.go').",
			},
		},
		"required": []string{"pattern"},
	})
	addDef("grep", "Search for a pattern in file contents recursively.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The pattern to search for.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory or file to search in.",
			},
		},
		"required": []string{"pattern", "path"},
	})
	addDef("shell", "Execute a shell command with a configurable timeout. Note: the shell is NOT path-sandboxed; only the approval gate constrains it.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
		},
		"required": []string{"command"},
	})
	addDef("fetch_url", "Fetch the content of a URL via HTTP GET.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
		},
		"required": []string{"url"},
	})
	addDef("list_directory", "List the contents of a directory at the given path.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the directory to list.",
			},
		},
		"required": []string{"path"},
	})

	return defs
}

func marshalParams(v map[string]any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal tool params: %w", err)
	}
	return b, nil
}

// Execute runs a tool call and returns the result.
func (e *BuiltInToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	result := api.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
	}

	switch call.Name {
	case "read_file":
		args, err := decodeArgs[readFileArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execReadFile(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "write_file":
		args, err := decodeArgs[writeFileArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execWriteFile(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "str_replace_file":
		args, err := decodeArgs[strReplaceArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execStrReplaceFile(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "glob":
		args, err := decodeArgs[globArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execGlob(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "grep":
		args, err := decodeArgs[grepArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execGrep(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "shell":
		args, err := decodeArgs[shellArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execShell(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "fetch_url":
		args, err := decodeArgs[fetchURLArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execFetchURL(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	case "list_directory":
		args, err := decodeArgs[listDirArgs](call.Arguments)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Output, err = e.execListDirectory(ctx, args)
			if err != nil {
				result.Error = err.Error()
			}
		}
	default:
		result.Error = fmt.Sprintf("unknown tool: %s", call.Name)
	}

	return result, nil
}

func (e *BuiltInToolExecutor) execReadFile(ctx context.Context, args readFileArgs) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("read file cancelled: %w", err)
	}
	if args.Path == "" {
		return "", ErrPathRequired
	}
	validPath, err := e.validatePath(args.Path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	var info os.FileInfo
	var f *os.File
	if e.root != nil {
		info, err = e.root.Stat(validPath)
		if err != nil {
			if isRootEscapeErr(err) {
				return "", fmt.Errorf("read file: %w: path escapes sandbox", ErrSandboxViolation)
			}
			return "", fmt.Errorf("read file: %w", err)
		}
		if info.Size() > maxFileReadSize {
			return "", fmt.Errorf("file exceeds max read size of %d bytes", maxFileReadSize)
		}
		f, err = e.root.Open(validPath)
		if err != nil {
			if isRootEscapeErr(err) {
				return "", fmt.Errorf("read file: %w: path escapes sandbox", ErrSandboxViolation)
			}
			return "", fmt.Errorf("read file: %w", err)
		}
		if err := checkFileHardlinkEscape(f); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("read file: %w", err)
		}
	} else {
		info, err = os.Stat(validPath)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		if info.Size() > maxFileReadSize {
			return "", fmt.Errorf("file exceeds max read size of %d bytes", maxFileReadSize)
		}
		f, err = openFileNoFollow(validPath)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// isRootEscapeErr reports whether err is an os.Root "path escapes from parent"
// error.
func isRootEscapeErr(err error) bool {
	var pe *os.PathError
	if errors.As(err, &pe) {
		// os.Root does not export a sentinel for this error, so we match the
		// documented text. This will need updating if Go changes the message.
		return pe.Err != nil && strings.Contains(pe.Err.Error(), "path escapes from parent")
	}
	return false
}

// randomTempName returns a random name suitable for a temporary file inside
// the sandbox root.
func randomTempName() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf(".kimi-%d", time.Now().UnixNano())
	}
	return ".kimi-" + hex.EncodeToString(b)
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

// atomicWriteFileRoot is the os.Root counterpart to atomicWriteFile.
// The target path is relative to the sandbox root. If skipHardlinkCheck is
// true, the existing target is not re-opened for a hardlink escape check; the
// caller is responsible for having already checked the file.
func (e *BuiltInToolExecutor) atomicWriteFileRoot(relTarget string, data []byte, skipHardlinkCheck bool) error {
	dir := filepath.Dir(relTarget)
	if dir != "." && dir != "" {
		if err := e.root.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	mode := os.FileMode(0600)
	if info, err := e.root.Stat(relTarget); err == nil {
		mode = info.Mode().Perm()
		// Best-effort hardlink check on the existing target. A regular file with
		// multiple links may alias a path outside the root.
		if !skipHardlinkCheck && info.Mode().IsRegular() {
			tf, err := e.root.Open(relTarget)
			if err == nil {
				if hErr := checkFileHardlinkEscape(tf); hErr != nil {
					_ = tf.Close()
					return fmt.Errorf("write file: %w", hErr)
				}
				_ = tf.Close()
			}
		}
	}

	tmpRel := filepath.Join(dir, randomTempName())
	f, err := e.root.OpenFile(tmpRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = e.root.Remove(tmpRel)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = e.root.Remove(tmpRel)
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = e.root.Remove(tmpRel)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := e.root.Chmod(tmpRel, mode); err != nil {
		_ = e.root.Remove(tmpRel)
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := e.root.Rename(tmpRel, relTarget); err != nil {
		_ = e.root.Remove(tmpRel)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func (e *BuiltInToolExecutor) execWriteFile(ctx context.Context, args writeFileArgs) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("write file cancelled: %w", err)
	}
	if args.Path == "" {
		return "", ErrPathRequired
	}
	if args.Content == "" {
		return "", fmt.Errorf("content is required")
	}
	if len(args.Content) > maxFileWriteSize {
		return "", fmt.Errorf("content exceeds max size of %d bytes", maxFileWriteSize)
	}
	validPath, err := e.validatePath(args.Path)
	if err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	if e.root != nil {
		if err := e.atomicWriteFileRoot(validPath, []byte(args.Content), false); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	} else {
		if err := atomicWriteFile(validPath, []byte(args.Content)); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}

func (e *BuiltInToolExecutor) execStrReplaceFile(ctx context.Context, args strReplaceArgs) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("replace file cancelled: %w", err)
	}
	if args.Path == "" {
		return "", ErrPathRequired
	}
	if args.OldString == "" {
		return "", fmt.Errorf("old_string is required")
	}
	validPath, err := e.validatePath(args.Path)
	if err != nil {
		return "", fmt.Errorf("str_replace_file: %w", err)
	}

	var data []byte
	if e.root != nil {
		f, err := e.root.Open(validPath)
		if err != nil {
			if isRootEscapeErr(err) {
				return "", fmt.Errorf("str_replace_file: %w: path escapes sandbox", ErrSandboxViolation)
			}
			return "", fmt.Errorf("read file: %w", err)
		}
		if err := checkFileHardlinkEscape(f); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("str_replace_file: %w", err)
		}
		data, err = io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
	} else {
		f, err := openFileNoFollow(validPath)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		defer func() { _ = f.Close() }()
		data, err = io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
	}

	content := string(data)
	if !strings.Contains(content, args.OldString) {
		return "", fmt.Errorf("old_string not found in file")
	}
	content = strings.ReplaceAll(content, args.OldString, args.NewString)
	if len(content) > maxFileWriteSize {
		return "", fmt.Errorf("result exceeds max size of %d bytes", maxFileWriteSize)
	}
	if e.root != nil {
		if err := e.atomicWriteFileRoot(validPath, []byte(content), true); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	} else {
		if err := atomicWriteFile(validPath, []byte(content)); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	}
	return fmt.Sprintf("replaced in %s", args.Path), nil
}

func (e *BuiltInToolExecutor) execGlob(ctx context.Context, args globArgs) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("glob cancelled: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	if e.root != nil {
		pattern := args.Pattern
		if filepath.IsAbs(pattern) {
			// Resolve the directory part of the pattern so that symlinked
			// directories (e.g., /tmp on macOS) are mapped into the root
			// namespace, while preserving the glob suffix.
			dir := filepath.Dir(pattern)
			glob := filepath.Base(pattern)
			validatedDir, err := ValidateFilePath(dir, e.sandboxRoot, e.protectedPaths)
			if err != nil {
				return "", nil // drop out-of-sandbox patterns
			}
			relDir, err := filepath.Rel(e.sandboxRoot, validatedDir)
			if err != nil || relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) {
				return "", nil
			}
			pattern = filepath.ToSlash(filepath.Join(relDir, glob))
		} else {
			pattern = filepath.ToSlash(pattern)
		}

		matches, err := fs.Glob(e.root.FS(), pattern)
		if err != nil {
			return "", fmt.Errorf("glob: %w", err)
		}
		var valid []string
		for _, m := range matches {
			abs := filepath.Join(e.sandboxRoot, filepath.FromSlash(m))
			valid = append(valid, abs)
		}
		return strings.Join(valid, "\n"), nil
	}

	// Resolve relative patterns against sandboxRoot.
	pattern := args.Pattern
	if !filepath.IsAbs(pattern) && e.sandboxRoot != "" {
		pattern = filepath.Join(e.sandboxRoot, pattern)
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}

	var valid []string
	for _, m := range matches {
		if _, vErr := e.validatePath(m); vErr != nil {
			continue // drop out-of-sandbox matches
		}
		valid = append(valid, m)
	}
	return strings.Join(valid, "\n"), nil
}

func (e *BuiltInToolExecutor) execGrep(_ context.Context, args grepArgs) (string, error) {
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if args.Path == "" {
		return "", ErrPathRequired
	}
	validPath, err := e.validatePath(args.Path)
	if err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	if e.root != nil {
		return e.execGrepRoot(validPath, re)
	}

	var results []string
	var totalBytes int

	walkFn := func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		// Validate the file path (catches escapes via symlinks or traversal).
		if _, vErr := e.validatePath(filePath); vErr != nil {
			return nil
		}

		info, sErr := d.Info()
		if sErr != nil {
			return nil
		}
		if info.Size() > maxFileReadSize {
			return nil // skip files exceeding read cap
		}

		f, openErr := openFileNoFollow(filePath)
		if openErr != nil {
			return nil
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), maxFileReadSize)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
			if re.MatchString(line) {
				relPath, relErr := filepath.Rel(validPath, filePath)
				if relErr != nil {
					relPath = filePath
				}
				result := fmt.Sprintf("%s:%d:%s", relPath, lineNum, line)
				results = append(results, result)
				totalBytes += len(result) + 1
				if totalBytes > maxShellOutputSize {
					return errOutputLimitReached
				}
			}
			lineNum++
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		return nil
	}

	// WalkDir handles both files and directories.
	walkErr := filepath.WalkDir(validPath, walkFn)
	if walkErr != nil {
		if errors.Is(walkErr, errOutputLimitReached) {
			// Truncate and add notice.
			results = append(results, fmt.Sprintf("... truncated (%d bytes total)", totalBytes))
		} else {
			return "", fmt.Errorf("grep: %w", walkErr)
		}
	}

	if len(results) == 0 {
		return "", nil
	}
	return strings.Join(results, "\n"), nil
}

func (e *BuiltInToolExecutor) execGrepRoot(relPath string, re *regexp.Regexp) (string, error) {
	var results []string
	var totalBytes int

	walkFn := func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		// Skip symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, sErr := d.Info()
		if sErr != nil {
			return nil
		}
		if info.Size() > maxFileReadSize {
			return nil // skip files exceeding read cap
		}

		f, openErr := e.root.Open(filePath)
		if openErr != nil {
			return nil
		}
		if hErr := checkFileHardlinkEscape(f); hErr != nil {
			_ = f.Close()
			return nil
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), maxFileReadSize)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
			if re.MatchString(line) {
				relOut := filePath
				if relPath != "." {
					if r, relErr := filepath.Rel(relPath, filepath.FromSlash(filePath)); relErr == nil && r != "" && r != "." {
						relOut = r
					}
				}
				result := fmt.Sprintf("%s:%d:%s", relOut, lineNum, line)
				results = append(results, result)
				totalBytes += len(result) + 1
				if totalBytes > maxShellOutputSize {
					_ = f.Close()
					return errOutputLimitReached
				}
			}
			lineNum++
		}
		if err := scanner.Err(); err != nil {
			_ = f.Close()
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		_ = f.Close()
		return nil
	}

	walkErr := fs.WalkDir(e.root.FS(), relPath, walkFn)
	if walkErr != nil {
		if errors.Is(walkErr, errOutputLimitReached) {
			results = append(results, fmt.Sprintf("... truncated (%d bytes total)", totalBytes))
		} else {
			return "", fmt.Errorf("grep: %w", walkErr)
		}
	}

	if len(results) == 0 {
		return "", nil
	}
	return strings.Join(results, "\n"), nil
}

func (e *BuiltInToolExecutor) execShell(ctx context.Context, args shellArgs) (string, error) {
	if args.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	if len(args.Command) > maxShellCommandLen {
		return "", fmt.Errorf("command exceeds max length of %d bytes", maxShellCommandLen)
	}
	if !e.allowShell {
		return "", fmt.Errorf("shell tool is disabled")
	}

	ctx, cancel := context.WithTimeout(ctx, e.shellTimeout)
	defer cancel()

	// NOTE: shell is NOT path-sandboxed; cmd.Dir is a working directory, not a
	// chroot. The only guard is the approval gate, which never auto-approves
	// the shell tool regardless of configuration.
	cmd := exec.CommandContext(ctx, "sh", "-c", args.Command)
	cmd.Env = curatedEnv()
	if e.sandboxRoot != "" {
		cmd.Dir = e.sandboxRoot
	}
	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if len(outStr) > maxShellOutputSize {
		outStr = strings.ToValidUTF8(outStr[:maxShellOutputSize], "")
		outStr += fmt.Sprintf("\n... truncated (%d bytes total)", len(output))
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return outStr, fmt.Errorf("command timed out after %s", e.shellTimeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
			outStr += fmt.Sprintf("\n[exit status %d]", exitErr.ExitCode())
			return outStr, nil
		}
		return outStr, fmt.Errorf("shell: %w", err)
	}
	return outStr, nil
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

func (e *BuiltInToolExecutor) execListDirectory(ctx context.Context, args listDirArgs) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("list directory cancelled: %w", err)
	}
	if args.Path == "" {
		return "", ErrPathRequired
	}
	validPath, err := e.validatePath(args.Path)
	if err != nil {
		return "", fmt.Errorf("list directory: %w", err)
	}

	var entries []os.DirEntry
	if e.root != nil {
		entries, err = fs.ReadDir(e.root.FS(), validPath)
	} else {
		entries, err = os.ReadDir(validPath)
	}
	if err != nil {
		if e.root != nil && isRootEscapeErr(err) {
			return "", fmt.Errorf("list directory: %w: path escapes sandbox", ErrSandboxViolation)
		}
		return "", fmt.Errorf("list directory: %w", err)
	}
	var lines []string
	for _, entry := range entries {
		marker := "file"
		if entry.IsDir() {
			marker = "dir"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, entry.Name()))
	}
	return strings.Join(lines, "\n"), nil
}

func (e *BuiltInToolExecutor) execFetchURL(ctx context.Context, args fetchURLArgs) (string, error) {
	if args.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are allowed")
	}
	// The authoritative SSRF guard is DialContext/CheckRedirect in the HTTP client, not this pre-check.
	if netutil.IsBlockedHost(u.Hostname()) {
		return "", fmt.Errorf("URL host is blocked")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch url: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBodySize))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType := strings.TrimSpace(strings.Split(contentType, ";")[0])

	allowed := strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		mediaType == "application/xml"

	if !allowed {
		return fmt.Sprintf("Fetched %s content (%d bytes) — binary or non-text data omitted", mediaType, len(body)), nil
	}

	output := "--- BEGIN UNTRUSTED EXTERNAL DATA ---\n" + string(body)
	if len(body) == maxFetchBodySize {
		output += "\n... truncated (max " + fmt.Sprintf("%d", maxFetchBodySize) + " bytes)"
	}
	output += "\n--- END UNTRUSTED EXTERNAL DATA ---"
	return output, nil
}
