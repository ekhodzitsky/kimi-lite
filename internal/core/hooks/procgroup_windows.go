//go:build windows

package hooks

import (
	"fmt"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// A no-op at creation time. Windows does not provide a portable,
	// CGO-free way to put a newly-created child into its own job object
	// through the standard library. Termination is handled by taskkill
	// in killProcessGroupPID.
}

func killProcessGroupPID(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("taskkill process tree %d: %w", pid, err)
	}
	return nil
}
