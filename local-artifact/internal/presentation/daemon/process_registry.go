package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const processRegistryRootDir = "processes"
const processPIDFileSuffix = ".pid"

type processPIDRecord struct {
	PID     int    `json:"pid"`
	StartID string `json:"start_id"`
}

func ProcessRegistryRoleDir(stateDir, role string) string {
	baseDir := filepath.Join(stateDir, "daemon", processRegistryRootDir)
	safeRole, ok := sanitizeProcessRegistryRole(role)
	if !ok {
		return baseDir
	}
	return filepath.Join(baseDir, safeRole)
}

func RegisterProcessPID(stateDir, role string, pid int) (func() error, error) {
	safeRole, ok := sanitizeProcessRegistryRole(role)
	if !ok {
		return nil, fmt.Errorf("invalid process registry role %q", strings.TrimSpace(role))
	}
	if pid <= 0 {
		return nil, fmt.Errorf("invalid pid %d", pid)
	}

	roleDir := ProcessRegistryRoleDir(stateDir, safeRole)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		return nil, err
	}
	startID, err := processStartID(pid)
	if err != nil {
		return nil, fmt.Errorf("resolve process start identity for pid %d: %w", pid, err)
	}

	payload, err := json.Marshal(processPIDRecord{
		PID:     pid,
		StartID: startID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal process pid record: %w", err)
	}

	pidFilePath := filepath.Join(roleDir, pidFileName(pid))
	if err := writeFileAtomic(pidFilePath, append(payload, '\n'), 0o644); err != nil {
		return nil, err
	}

	return func() error {
		return UnregisterProcessPID(stateDir, safeRole, pid)
	}, nil
}

func UnregisterProcessPID(stateDir, role string, pid int) error {
	safeRole, ok := sanitizeProcessRegistryRole(role)
	if !ok {
		return fmt.Errorf("invalid process registry role %q", strings.TrimSpace(role))
	}
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	err := os.Remove(filepath.Join(ProcessRegistryRoleDir(stateDir, safeRole), pidFileName(pid)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sanitizeProcessRegistryRole(role string) (string, bool) {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" || filepath.IsAbs(trimmed) || strings.ContainsAny(trimmed, `/\`) {
		return "", false
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || cleaned != trimmed {
		return "", false
	}
	return cleaned, true
}

func pidFileName(pid int) string {
	return strconv.Itoa(pid) + processPIDFileSuffix
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		// Windows cannot atomically replace existing files via os.Rename.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		if retryErr := os.Rename(tmpPath, path); retryErr != nil {
			return retryErr
		}
	}

	cleanup = false
	return nil
}
