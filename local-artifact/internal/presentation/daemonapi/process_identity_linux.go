//go:build linux

package daemonapi

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const linuxProcStatStartFieldIndex = 19

func processStartID(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	end := strings.LastIndex(line, ")")
	if end == -1 || end+2 >= len(line) {
		return "", fmt.Errorf("unexpected /proc stat format")
	}
	fields := strings.Fields(line[end+2:])
	if len(fields) <= linuxProcStatStartFieldIndex {
		return "", fmt.Errorf("unexpected /proc stat field count")
	}
	startTimeField := fields[linuxProcStatStartFieldIndex]
	if _, err := strconv.ParseUint(startTimeField, 10, 64); err != nil {
		return "", fmt.Errorf("parse /proc stat start time: %w", err)
	}
	return startTimeField, nil
}
