package git

import (
	"context"
	"fmt"
)

// Status returns the current git status as a string.
func (p *Provider) Status(ctx context.Context) (string, error) {
	out, err := p.runner.CombinedOutput(ctx, p.dir, "git", "status")
	if err != nil {
		if isGitNotFound(err) {
			return "", fmt.Errorf("git is not installed: %w", err)
		}
		if isNotRepo(string(out)) {
			return "", fmt.Errorf("not a git repository: %w", err)
		}
		return "", fmt.Errorf("git status failed: %w", err)
	}
	return string(out), nil
}

// IsRepo returns true if the current directory is a git repository.
func (p *Provider) IsRepo(ctx context.Context) bool {
	_, err := p.runner.CombinedOutput(ctx, p.dir, "git", "rev-parse", "--git-dir")
	return err == nil
}
