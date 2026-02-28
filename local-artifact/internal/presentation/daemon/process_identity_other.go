//go:build !linux && !windows

package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func processStartID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	out, err := exec.Command("ps", "-o", "lstart=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	startID := strings.TrimSpace(string(out))
	if startID == "" {
		return "", fmt.Errorf("process %d not found", pid)
	}
	return startID, nil
}
