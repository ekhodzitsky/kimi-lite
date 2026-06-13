package git

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	if strings.HasPrefix(clean, "..") {
		return "", errors.New("git diff: path escapes working directory")
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git", "diff", "--no-color", "--", clean)
	if err != nil {
		if errCtx := ctx.Err(); errCtx != nil {
			if errors.Is(errCtx, context.Canceled) {
				return "", fmt.Errorf("git diff canceled: %w", errCtx)
			}
			return "", fmt.Errorf("git diff timed out: %w", errCtx)
		}
		return "", classifyErr("git diff", stderr, err)
	}
	return string(stdout), nil
}
