//go:build windows

// Package procgroup provides cross-platform process-group helpers used by the
// core executor and lifecycle hook runner.
package procgroup

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// killTimeout bounds the runtime of taskkill so that a stuck termination
// attempt cannot block the caller indefinitely.
const killTimeout = 10 * time.Second

// SetProcessGroup is a no-op on Windows. Process-tree termination is handled
// by taskkill in KillProcessGroupPID.
func SetProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroupPID terminates the process identified by pid and its
// descendants using taskkill /T /F. stdout/stderr are discarded and the command
// is bounded by a timeout to avoid blocking the caller.
func KillProcessGroupPID(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), killTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("taskkill process tree %d: %w", pid, err)
	}
	return nil
}
