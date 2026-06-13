//go:build !windows

package core

import (
	"fmt"
	"os"
	"syscall"
)

// openFileNoFollow opens path for reading without following symlinks.
// On Unix this uses O_NOFOLLOW to defend against TOCTOU symlink races.
func openFileNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// checkFileHardlinkEscape returns ErrSandboxViolation if f is a regular file
// with multiple hardlinks, which may alias a file outside the sandbox root.
func checkFileHardlinkEscape(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat opened file: %w", err)
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if ok && fi.Mode().IsRegular() && sys.Nlink > 1 {
		return fmt.Errorf("%w: file has multiple hardlinks", ErrSandboxViolation)
	}
	return nil
}
