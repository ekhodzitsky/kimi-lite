package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Status returns the current git status as a string.
func (p *Provider) Status(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git", "-c", "color.status=never", "status", "--porcelain")
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("git status timed out")
		}
		return "", classifyErr("git status", stderr, err)
	}
	return string(stdout), nil
}

// IsRepo returns true if the current directory is inside a git work tree.
// It returns (false, nil) for a genuine non-repository, and (false, err)
// for execution errors such as git-not-found or cancellation.
func (p *Provider) IsRepo(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, fmt.Errorf("git is-repo timed out")
		}
		if isGitNotFound(err) {
			return false, fmt.Errorf("git is not installed: %w", err)
		}
		if isNotRepo(string(stderr)) {
			return false, nil
		}
		return false, fmt.Errorf("git is-repo failed: %w", err)
	}
	return strings.TrimSpace(string(stdout)) == "true", nil
}
