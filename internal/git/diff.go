package git

import (
	"context"
	"fmt"
)

// Diff returns the diff for a specific file.
func (p *Provider) Diff(ctx context.Context, path string) (string, error) {
	out, err := p.runner.CombinedOutput(ctx, p.dir, "git", "diff", "--", path)
	if err != nil {
		if isGitNotFound(err) {
			return "", fmt.Errorf("git is not installed: %w", err)
		}
		if isNotRepo(string(out)) {
			return "", fmt.Errorf("not a git repository: %w", err)
		}
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	return string(out), nil
}
