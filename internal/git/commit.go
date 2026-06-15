package git

import (
	"context"
	"strings"
)

// Default commit identity used when the repository has no user configuration.
const (
	defaultCommitName  = "kimi-lite"
	defaultCommitEmail = "kimi-lite@localhost"
)

// Commit creates a git commit with the given message.
// It commits already-staged changes with a default identity and --no-verify,
// and treats a clean working tree ("nothing to commit") as success.
// Callers must stage changes themselves; this function does not auto-stage
// files, avoiding the risk of committing unexpected untracked content.
func (p *Provider) Commit(ctx context.Context, message string) error {
	if message == "" {
		message = "kimi-lite checkpoint"
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	stdout, stderr, err := p.runner.Output(ctx, p.dir, "git",
		"-c", "user.name="+defaultCommitName,
		"-c", "user.email="+defaultCommitEmail,
		"commit", "-m", message,
		"--no-verify",
	)
	if err != nil {
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
