package daemonctl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type registeredPID struct {
	pid         int
	pidFilePath string
	startID     string
}

type pidFileRecord struct {
	PID     int    `json:"pid"`
	StartID string `json:"start_id"`
}

func registryRoleDir(stateDir, role string) string {
	return filepath.Join(stateDir, "daemon", "processes", strings.TrimSpace(role))
}

func listRolePIDs(stateDir, role string) ([]registeredPID, []string, error) {
	roleDir := registryRoleDir(stateDir, role)
	entries, err := os.ReadDir(roleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	registered := make([]registeredPID, 0, len(entries))
	invalidPaths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(roleDir, entry.Name())
		pid, ok := parsePIDFileName(entry.Name())
		if !ok || pid <= 0 {
			invalidPaths = append(invalidPaths, path)
			continue
		}

		record, ok := parsePIDFile(path, pid)
		if !ok {
			invalidPaths = append(invalidPaths, path)
			continue
		}

		registered = append(registered, registeredPID{
			pid:         pid,
			pidFilePath: path,
			startID:     record.StartID,
		})
	}

	return registered, invalidPaths, nil
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

func parsePIDFile(path string, expectedPID int) (pidFileRecord, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pidFileRecord{}, false
	}

	var record pidFileRecord
	if err := json.Unmarshal(bytes.TrimSpace(raw), &record); err != nil {
		return pidFileRecord{}, false
	}
	if record.PID != expectedPID {
		return pidFileRecord{}, false
	}
	record.StartID = strings.TrimSpace(record.StartID)
	if record.StartID == "" {
		return pidFileRecord{}, false
	}
	return record, true
}
