//go:build windows

package core

import "os/exec"

// setProcessGroup is a no-op on Windows; killing child processes is handled
// by the exec package's context cancellation.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroupPID is a no-op on Windows.
func killProcessGroupPID(pid int) error { return nil }
