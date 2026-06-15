// Package git provides an api.GitProvider implementation that shells out to git.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultGitTimeout = 30 * time.Second

// gitEnvOverlay contains the environment variables forced for every git
// command. It is precomputed once to avoid rebuilding the slice per call.
var gitEnvOverlay = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_PAGER=cat",
	"LC_ALL=C",
}

// gitEnvSanitize lists variables that must be stripped from the parent
// environment before applying gitEnvOverlay, so callers cannot redirect git to
// an unexpected repository or configuration.
var gitEnvSanitize = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_CONFIG_GLOBAL",
	"GIT_CONFIG_SYSTEM",
}

// cmdRunner abstracts command execution for testability.
type cmdRunner interface {
	// Output runs a command in dir and returns its stdout, stderr, and any error.
	Output(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error)
}

// execRunner is the production implementation using os/exec.
type execRunner struct{}

// Output implements cmdRunner using exec.CommandContext with separate stdout/stderr.
func (r *execRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("git provider: invalid directory %q: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("git provider: %q is not a directory", dir)
		}
	}

	cmd := r.buildCmd(ctx, dir, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (r *execRunner) buildCmd(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = nil
	cmd.Env = append(sanitizeEnv(os.Environ(), gitEnvSanitize), gitEnvOverlay...)
	return cmd
}

func sanitizeEnv(env []string, remove []string) []string {
	rm := make(map[string]struct{}, len(remove))
	for _, k := range remove {
		rm[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		if k, _, ok := strings.Cut(e, "="); ok {
			if _, found := rm[k]; found {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// Provider implements api.GitProvider by executing git commands.
type Provider struct {
	runner  cmdRunner
	dir     string
	timeout time.Duration
}

// NewProvider creates a new Git provider that operates in dir with the default
// command timeout. If dir is empty, git commands run in the current working
// directory. The directory is validated when a command is actually executed;
// an invalid directory returns an error from the operation rather than from
// NewProvider, preserving the existing constructor signature.
func NewProvider(dir string) *Provider {
	return NewProviderWithTimeout(dir, defaultGitTimeout)
}

// NewProviderWithTimeout creates a new Git provider that operates in dir with
// the given command timeout. A non-positive timeout is replaced by the default.
func NewProviderWithTimeout(dir string, timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	return &Provider{
		runner:  &execRunner{},
		dir:     dir,
		timeout: timeout,
	}
}

// newProvider creates a new Git provider with the given runner for testing.
func newProvider(runner cmdRunner, dir string, timeout ...time.Duration) *Provider {
	t := defaultGitTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		t = timeout[0]
	}
	return &Provider{
		runner:  runner,
		dir:     dir,
		timeout: t,
	}
}

// Dir returns the working directory the provider operates in.
func (p *Provider) Dir() string {
	return p.dir
}

// isGitNotFound reports whether err indicates that git is not installed.
func isGitNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// isNotRepo reports whether output indicates the directory is not a git repository.
func isNotRepo(output string) bool {
	return strings.Contains(output, "not a git repository")
}

// classifyErr returns a wrapped, classified error for git operation failures.
// The captured stderr is included in the message so callers can see why git
// failed without losing the underlying error.
func classifyErr(op string, out []byte, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("git is not installed: %w", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out: %w", op, err)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s canceled: %w", op, err)
	}
	msg := strings.TrimSpace(string(out))
	if isNotRepo(msg) {
		return fmt.Errorf("not a git repository: %w", err)
	}
	if msg != "" {
		return fmt.Errorf("%s failed: %s: %w", op, msg, err)
	}
	return fmt.Errorf("%s failed: %w", op, err)
}
