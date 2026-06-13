//go:build windows

package hooks

import "os/exec"

func setProcessGroup(cmd *exec.Cmd)     {}
func killProcessGroupPID(pid int) error { return nil }
