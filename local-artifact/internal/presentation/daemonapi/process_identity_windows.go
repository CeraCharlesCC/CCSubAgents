//go:build windows

package daemonapi

import (
	"fmt"
	"strconv"
	"syscall"
)

func processStartID(pid int) (string, error) {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(handle)

	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return "", err
	}

	return strconv.FormatUint(uint64(creation.Nanoseconds()), 10), nil
}
