//go:build !windows

package core

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in a new process group so that child
// processes spawned by the shell inherit the group and can be killed together.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroupPID sends SIGKILL to the entire process group identified by pid.
func killProcessGroupPID(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}
