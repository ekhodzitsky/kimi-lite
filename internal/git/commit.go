package git

import (
	"context"
	"errors"
	"fmt"
)

// Commit creates a git commit with the given message.
func (p *Provider) Commit(ctx context.Context, message string) error {
	if message == "" {
		message = "kimi-lite checkpoint"
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	_, stderr, err := p.runner.Output(ctx, p.dir, "git", "add", "-A")
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("git add timed out")
		}
		return classifyErr("git add", stderr, err)
	}

	_, stderr, err = p.runner.Output(ctx, p.dir, "git", "commit", "-m", message)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("git commit timed out")
		}
		return classifyErr("git commit", stderr, err)
	}

	return nil
}
