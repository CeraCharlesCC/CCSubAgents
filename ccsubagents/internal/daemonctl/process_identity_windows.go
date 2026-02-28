//go:build windows

package daemonctl

import (
	"fmt"
	"strconv"
	"syscall"
)

func processStartID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}

	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(handle)

	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return "", err
	}

	startID := (uint64(creation.HighDateTime) << 32) | uint64(creation.LowDateTime)
	return strconv.FormatUint(startID, 10), nil
}
