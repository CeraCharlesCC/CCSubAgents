//go:build windows

package main

import "os"

func sendProcessGraceful(proc *os.Process) error {
	return proc.Signal(os.Interrupt)
}

func sendProcessForce(proc *os.Process) error {
	return proc.Kill()
}
