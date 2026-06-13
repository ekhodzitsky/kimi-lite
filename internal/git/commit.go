package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Default commit identity used when the repository has no user configuration.
const (
	defaultCommitName  = "kimi-lite"
	defaultCommitEmail = "kimi-lite@localhost"
)

// Commit creates a git commit with the given message.
// It stages all changes, commits with a default identity and --no-verify, and
// treats a clean working tree ("nothing to commit") as success.
func (p *Provider) Commit(ctx context.Context, message string) error {
	if message == "" {
		message = "kimi-lite checkpoint"
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	_, stderr, err := p.runner.Output(ctx, p.dir, "git", "add", "-A")
	if err != nil {
		if errCtx := ctx.Err(); errCtx != nil {
			if errors.Is(errCtx, context.Canceled) {
				return fmt.Errorf("git add canceled: %w", errCtx)
			}
			return fmt.Errorf("git add timed out: %w", errCtx)
		}
		return classifyErr("git add", stderr, err)
	}

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git",
		"-c", "user.name="+defaultCommitName,
		"-c", "user.email="+defaultCommitEmail,
		"commit", "-m", message,
		"--no-verify",
	)
	if err != nil {
		if errCtx := ctx.Err(); errCtx != nil {
			if errors.Is(errCtx, context.Canceled) {
				return fmt.Errorf("git commit canceled: %w", errCtx)
			}
			return fmt.Errorf("git commit timed out: %w", errCtx)
		}
		combined := string(stdout) + string(stderr)
		if isNothingToCommit(combined) {
			return nil
		}
		return classifyErr("git commit", stderr, err)
	}

	return nil
}

// isNothingToCommit reports whether git's output indicates a clean tree.
func isNothingToCommit(output string) bool {
	return strings.Contains(output, "nothing to commit") || strings.Contains(output, "nothing added to commit")
}
