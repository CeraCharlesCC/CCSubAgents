package daemonctl

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func registryRoleDir(stateDir, role string) string {
	return filepath.Join(stateDir, "daemon", "processes", strings.TrimSpace(role))
}

func listRolePIDs(stateDir, role string) ([]int, []string, error) {
	roleDir := registryRoleDir(stateDir, role)
	entries, err := os.ReadDir(roleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	pids := make([]int, 0, len(entries))
	pidFilePaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		pid, ok := parsePIDFileName(entry.Name())
		if !ok || pid <= 0 {
			continue
		}
		pids = append(pids, pid)
		pidFilePaths = append(pidFilePaths, filepath.Join(roleDir, entry.Name()))
	}

	return pids, pidFilePaths, nil
}

func parsePIDFileName(name string) (int, bool) {
	if !strings.HasSuffix(name, ".pid") {
		return 0, false
	}
	rawPID := strings.TrimSuffix(name, ".pid")
	if strings.TrimSpace(rawPID) == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(rawPID)
	if err != nil {
		return 0, false
	}
	return pid, true
}
