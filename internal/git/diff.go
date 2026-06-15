package git

import (
	"context"
	"errors"
	"path/filepath"
)

// Diff returns the diff for a specific file.
func (p *Provider) Diff(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", errors.New("git diff: empty path")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("git diff: absolute path not allowed")
	}
	clean := filepath.Clean(path)
	if !filepath.IsLocal(clean) {
		return "", errors.New("git diff: path escapes working directory")
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git", "diff", "--no-color", "--", clean)
	if err != nil {
		return "", classifyErr("git diff", stderr, err)
	}
	return string(stdout), nil
}
