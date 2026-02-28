//go:build !windows

package main

import (
	"os"
	"syscall"
)

func sendProcessGraceful(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

func sendProcessForce(proc *os.Process) error {
	return proc.Signal(syscall.SIGKILL)
}
