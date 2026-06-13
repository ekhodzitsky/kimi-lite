package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// defaultHookTimeout is the maximum runtime for a hook when no timeout
// is configured explicitly.
const defaultHookTimeout = 30 * time.Second

// execHook runs a single hook command with the configured timeout,
// environment variables, and output capture. cfg.Args are expected to
// be already rendered by the caller.
func execHook(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	//nolint:gosec // Commands are user-configured lifecycle hooks; arguments are rendered from HookData.
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = buildEnv(cfg.Env, data)
	cmd.Dir = "."

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook timed out after %v", timeout)
		}
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("hook canceled: %w", err)
		}
		return fmt.Errorf("hook exited with %v: %s", err, out.String())
	}
	return nil
}
