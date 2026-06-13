package hooks

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// defaultHookTimeout is the maximum runtime for a hook when no timeout
// is configured explicitly.
const defaultHookTimeout = 30 * time.Second

// maxHookOutputSize caps the bytes captured from a hook's stdout/stderr.
const maxHookOutputSize = 1 << 20 // 1 MiB

// waitDelay gives a process a short grace period after the context is done
// before cmd.Wait returns forcefully.
const hookWaitDelay = 5 * time.Second

// limitedWriter accumulates output up to a fixed byte limit. Writes beyond
// the limit are counted but discarded, and Truncated() reports whether any
// output was dropped.
type limitedWriter struct {
	buf       []byte
	limit     int
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return len(p), nil
	}
	if len(w.buf) >= w.limit {
		w.truncated = true
		return len(p), nil
	}
	remain := w.limit - len(w.buf)
	if remain > len(p) {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	w.buf = append(w.buf, p[:remain]...)
	w.truncated = true
	return len(p), nil
}

func (w *limitedWriter) Bytes() []byte { return w.buf }

func (w *limitedWriter) Truncated() bool { return w.truncated }

// runCommandWithContext runs cmd and kills its entire process group when ctx is
// cancelled or reaches its deadline, ensuring child processes are terminated.
func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("context cancelled: %w", err)
	}
	setProcessGroup(cmd)

	out := &limitedWriter{limit: maxHookOutputSize}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = hookWaitDelay

	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("start command: %w", err)
	}

	done := make(chan struct{})
	go func(pid int) {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		select {
		case <-done:
		default:
			_ = killProcessGroupPID(pid)
		}
	}(cmd.Process.Pid)

	err := cmd.Wait()
	close(done)
	return out.Bytes(), out.Truncated(), err
}

func execHook(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}

	// Capture parent context state before layering the hook timeout on top.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("hook aborted by parent context: %w", err)
	}
	parentCtx := ctx

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	//nolint:gosec // Commands are user-configured lifecycle hooks; arguments are rendered from HookData.
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = buildEnv(cfg.Env, data)
	cmd.Dir = "."

	out, truncated, err := runCommandWithContext(ctx, cmd)
	if err != nil {
		if parentCtx.Err() != nil {
			return fmt.Errorf("hook aborted by parent context: %w", parentCtx.Err())
		}
		if ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("hook timed out after %v", timeout)
			if truncated {
				msg += " (output truncated)"
			}
			return fmt.Errorf("%s", msg)
		}
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("hook canceled: %w", err)
		}
		output := string(out)
		if truncated {
			output += " [truncated]"
		}
		return fmt.Errorf("hook exited with %w: %s", err, output)
	}
	return nil
}
