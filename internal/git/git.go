// Package git provides an api.GitProvider implementation that shells out to git.
package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// cmdRunner abstracts command execution for testability.
type cmdRunner interface {
	// CombinedOutput runs a command in dir and returns its combined stdout and stderr.
	CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// execRunner is the production implementation using os/exec.
type execRunner struct{}

// CombinedOutput implements cmdRunner using exec.CommandContext.
func (r *execRunner) CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}

// Provider implements api.GitProvider by executing git commands.
type Provider struct {
	runner cmdRunner
	dir    string
}

// NewProvider creates a new Git provider that operates in dir.
// If dir is empty, git commands run in the current working directory.
func NewProvider(dir string) *Provider {
	return &Provider{
		runner: &execRunner{},
		dir:    dir,
	}
}

// newProvider creates a new Git provider with the given runner for testing.
func newProvider(runner cmdRunner, dir string) *Provider {
	return &Provider{
		runner: runner,
		dir:    dir,
	}
}

// isGitNotFound reports whether err indicates that git is not installed.
func isGitNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// isNotRepo reports whether output indicates the directory is not a git repository.
func isNotRepo(output string) bool {
	return strings.Contains(output, "not a git repository")
}
