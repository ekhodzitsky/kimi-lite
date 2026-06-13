//go:build windows

package core

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// openFileNoFollow opens path for reading without following symlinks or junctions.
// It uses FILE_FLAG_OPEN_REPARSE_POINT and then verifies the opened handle is not
// a reparse point.
func openFileNoFollow(path string) (*os.File, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	handle, err := syscall.CreateFile(
		pathp,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, &os.PathError{Op: "open", Path: path, Err: windows.ERROR_CANT_ACCESS_FILE}
	}

	return os.NewFile(uintptr(handle), path), nil
}

// checkFileHardlinkEscape returns ErrSandboxViolation if f is a regular file
// with multiple hardlinks, which may alias a file outside the sandbox root.
func checkFileHardlinkEscape(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat opened file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	handle := syscall.Handle(f.Fd())
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		// If we cannot determine the link count, allow the operation.
		return nil
	}
	if info.NumberOfLinks > 1 {
		return fmt.Errorf("%w: file has multiple hardlinks", ErrSandboxViolation)
	}
	return nil
}
