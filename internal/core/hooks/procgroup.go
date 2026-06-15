package hooks

import (
	"os/exec"

	"github.com/ekhodzitsky/kimi-lite/internal/core/procgroup"
)

// setProcessGroup puts the command in a new process group so that child
// processes spawned by the shell inherit the group and can be killed together.
func setProcessGroup(cmd *exec.Cmd) {
	procgroup.SetProcessGroup(cmd)
}

// killProcessGroupPID terminates the process identified by pid and its
// descendants.
func killProcessGroupPID(pid int) error {
	return procgroup.KillProcessGroupPID(pid)
}
