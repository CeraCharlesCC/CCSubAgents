//go:build windows

package daemonctl

import (
	"errors"
	"os"
	"syscall"
)

const (
	windowsErrorInvalidParameter syscall.Errno = 87
	windowsProcessStillActive    uint32        = 259
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == windowsErrorInvalidParameter {
			return false
		}
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return true
		}
		return false
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == windowsProcessStillActive
}

func sendGraceful(pid int) error {
	// Windows does not support POSIX-style graceful signals for arbitrary processes.
	// Fall back to force termination.
	return sendForce(pid)
}

func sendForce(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
