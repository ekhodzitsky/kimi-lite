package core

import (
	"fmt"
	"os/exec"

	"github.com/ekhodzitsky/kimi-lite/internal/core/procgroup"
)

// setProcessGroup puts the command in a new process group so that child
// processes spawned by the shell inherit the group and can be killed together.
func setProcessGroup(cmd *exec.Cmd) {
	procgroup.SetProcessGroup(cmd)
}

// killProcessGroupPID sends SIGKILL to the entire process group identified by pid.
func killProcessGroupPID(pid int) error {
	if err := procgroup.KillProcessGroupPID(pid); err != nil {
		return fmt.Errorf("kill process group: %w", err)
	}
	return nil
}
