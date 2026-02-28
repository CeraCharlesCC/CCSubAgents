//go:build linux

package daemonctl

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const linuxProcStatStartFieldIndex = 19

func processStartID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}

	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	raw, err := os.ReadFile(statPath)
	if err != nil {
		return "", err
	}

	line := strings.TrimSpace(string(raw))
	commEnd := strings.LastIndex(line, ")")
	if commEnd == -1 || commEnd+1 >= len(line) {
		return "", fmt.Errorf("unexpected stat format in %s", statPath)
	}

	fields := strings.Fields(line[commEnd+1:])
	if len(fields) <= linuxProcStatStartFieldIndex {
		return "", fmt.Errorf("unexpected stat field count in %s: %d", statPath, len(fields))
	}

	startID := fields[linuxProcStatStartFieldIndex]
	if _, err := strconv.ParseUint(startID, 10, 64); err != nil {
		return "", fmt.Errorf("parse process start id from %s: %w", statPath, err)
	}

	return startID, nil
}
