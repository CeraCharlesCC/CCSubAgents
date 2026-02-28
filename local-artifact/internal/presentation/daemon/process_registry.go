package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const processRegistryRootDir = "processes"

func ProcessRegistryRoleDir(stateDir, role string) string {
	return filepath.Join(stateDir, "daemon", processRegistryRootDir, strings.TrimSpace(role))
}

func RegisterProcessPID(stateDir, role string, pid int) (func() error, error) {
	if strings.TrimSpace(role) == "" {
		return nil, fmt.Errorf("process registry role is required")
	}
	if pid <= 0 {
		return nil, fmt.Errorf("invalid pid %d", pid)
	}

	roleDir := ProcessRegistryRoleDir(stateDir, role)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		return nil, err
	}
	pidFilePath := filepath.Join(roleDir, pidFileName(pid))
	if err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return nil, err
	}

	return func() error {
		return UnregisterProcessPID(stateDir, role, pid)
	}, nil
}

func UnregisterProcessPID(stateDir, role string, pid int) error {
	if strings.TrimSpace(role) == "" {
		return fmt.Errorf("process registry role is required")
	}
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	err := os.Remove(filepath.Join(ProcessRegistryRoleDir(stateDir, role), pidFileName(pid)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func pidFileName(pid int) string {
	return strconv.Itoa(pid) + ".pid"
}
