//go:build !windows

// Package procgroup provides cross-platform process-group helpers used by the
// core executor and lifecycle hook runner.
package procgroup

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// SetProcessGroup puts the command in a new process group so that child
// processes spawned by the shell inherit the group and can be killed together.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroupPID sends SIGKILL to the entire process group identified by pid.
func KillProcessGroupPID(pid int) error {
	if err := unix.Kill(-pid, unix.SIGKILL); err != nil {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}
