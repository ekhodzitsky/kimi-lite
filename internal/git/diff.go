package git

import (
	"context"
	"errors"
	"fmt"
)

// Diff returns the diff for a specific file.
func (p *Provider) Diff(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", errors.New("git diff: empty path")
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git", "diff", "--", path)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("git diff timed out")
		}
		return "", classifyErr("git diff", stderr, err)
	}
	return string(stdout), nil
}
