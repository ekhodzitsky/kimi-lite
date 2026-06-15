package core

import (
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
	return procgroup.KillProcessGroupPID(pid)
}
