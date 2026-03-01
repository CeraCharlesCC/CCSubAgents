package daemonctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const processPIDFileSuffix = ".pid"

type registeredPID struct {
	pid         int
	pidFilePath string
	startID     string
}

type rolePIDListing struct {
	registered []registeredPID
	issues     []error
}

type pidFileRecord struct {
	PID     int    `json:"pid"`
	StartID string `json:"start_id"`
}

func registryRoleDir(stateDir, role string) string {
	baseDir := filepath.Join(stateDir, "daemon", "processes")
	safeRole, ok := sanitizeRegistryRole(role)
	if !ok {
		return baseDir
	}
	return filepath.Join(baseDir, safeRole)
}

func sanitizeRegistryRole(role string) (string, bool) {
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

func listRolePIDs(stateDir, role string) (rolePIDListing, error) {
	roleDir := registryRoleDir(stateDir, role)
	entries, err := os.ReadDir(roleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return rolePIDListing{}, nil
		}
		return rolePIDListing{}, err
	}

	listing := rolePIDListing{
		registered: make([]registeredPID, 0, len(entries)),
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(roleDir, entry.Name())

		pid, isPIDFile, parseNameErr := parsePIDFileName(entry.Name())
		if parseNameErr != nil {
			listing.issues = append(listing.issues, fmt.Errorf("%w: skip invalid pid filename %s: %v", ErrProcessRegistryMetadata, path, parseNameErr))
			continue
		}
		if !isPIDFile {
			continue
		}

		record, parseFileErr := parsePIDFile(path, pid)
		if parseFileErr != nil {
			listing.issues = append(listing.issues, fmt.Errorf("%w: skip unreadable pid file %s: %v", ErrProcessRegistryMetadata, path, parseFileErr))
			continue
		}

		listing.registered = append(listing.registered, registeredPID{
			pid:         pid,
			pidFilePath: path,
			startID:     record.StartID,
		})
	}

	return listing, nil
}

func parsePIDFileName(name string) (int, bool, error) {
	if !strings.HasSuffix(name, processPIDFileSuffix) {
		return 0, false, nil
	}
	rawPID := strings.TrimSuffix(name, processPIDFileSuffix)
	if strings.TrimSpace(rawPID) == "" {
		return 0, true, errors.New("missing pid")
	}
	pid, err := strconv.Atoi(rawPID)
	if err != nil {
		return 0, true, fmt.Errorf("parse pid: %w", err)
	}
	if pid <= 0 {
		return 0, true, fmt.Errorf("invalid pid %d", pid)
	}
	return pid, true, nil
}

func parsePIDFile(path string, expectedPID int) (pidFileRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pidFileRecord{}, err
	}

	var record pidFileRecord
	if err := json.Unmarshal(bytes.TrimSpace(raw), &record); err != nil {
		return pidFileRecord{}, fmt.Errorf("decode json: %w", err)
	}
	if record.PID != expectedPID {
		return pidFileRecord{}, fmt.Errorf("pid mismatch: got=%d want=%d", record.PID, expectedPID)
	}
	record.StartID = strings.TrimSpace(record.StartID)
	if record.StartID == "" {
		return pidFileRecord{}, errors.New("missing start id")
	}
	return record, nil
}
