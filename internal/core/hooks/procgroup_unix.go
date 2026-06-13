//go:build !windows

package hooks

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killProcessGroupPID(pid int) error {
	if err := unix.Kill(-pid, unix.SIGKILL); err != nil {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}
