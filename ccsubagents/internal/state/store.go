package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func TrackedStatePath(stateDir string) string {
	return filepath.Join(stateDir, TrackedFileName)
}

func LoadTrackedState(stateDir string) (*TrackedState, error) {
	trackedPath := TrackedStatePath(stateDir)
	b, err := os.ReadFile(trackedPath)
	if err != nil {
		return nil, fmt.Errorf("read tracked state %s: %w", trackedPath, err)
	}
	var state TrackedState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("parse tracked state %s: %w", trackedPath, err)
	}
	if state.Version == 0 {
		return nil, fmt.Errorf("tracked state %s is missing version", trackedPath)
	}
	if state.Version < TrackedSchemaVersion {
		state.Version = TrackedSchemaVersion
	}
	return &state, nil
}

func LoadTrackedStateForInstall(stateDir string) (*TrackedState, error) {
	state, err := LoadTrackedState(stateDir)
	if err == nil {
		return state, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("tracked state is unreadable; resolve %s and retry: %w", TrackedStatePath(stateDir), err)
}

func SaveTrackedState(stateDir string, state TrackedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tracked state: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(stateDir, ".tracked-*.json")
	if err != nil {
		return fmt.Errorf("create tracked temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tracked temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tracked temp file: %w", err)
	}

	if err := os.Rename(tmpPath, TrackedStatePath(stateDir)); err != nil {
		return fmt.Errorf("replace tracked state: %w", err)
	}
	return nil
}
