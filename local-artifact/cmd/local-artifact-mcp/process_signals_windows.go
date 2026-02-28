//go:build windows

package main

import "os"

func sendProcessGraceful(proc *os.Process) error {
	// Windows does not support POSIX-style graceful signals for arbitrary processes.
	// Fall back to force termination.
	return sendProcessForce(proc)
}

func sendProcessForce(proc *os.Process) error {
	return proc.Kill()
}
