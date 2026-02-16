package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type trackedState struct {
	Version     int            `json:"version"`
	Repo        string         `json:"repo"`
	ReleaseID   int64          `json:"releaseId"`
	ReleaseTag  string         `json:"releaseTag"`
	InstalledAt string         `json:"installedAt"`
	Managed     managedState   `json:"managed"`
	JSONEdits   trackedJSONOps `json:"jsonEdits"`
}

type managedState struct {
	Files []string `json:"files"`
	Dirs  []string `json:"dirs"`
}

type trackedJSONOps struct {
	Settings settingsEdit `json:"settings"`
	MCP      mcpEdit      `json:"mcp"`
}

type settingsEdit struct {
	File      string `json:"file"`
	AgentPath string `json:"agentPath"`
	Mode      string `json:"mode,omitempty"`
	Added     bool   `json:"added"`
}

type mcpEdit struct {
	File        string          `json:"file"`
	Key         string          `json:"key"`
	Touched     bool            `json:"touched"`
	HadPrevious bool            `json:"hadPrevious"`
	Previous    json.RawMessage `json:"previous,omitempty"`
}

func (m *Manager) trackedStatePath(stateDir string) string {
	return filepath.Join(stateDir, trackedFileName)
}

func (m *Manager) loadTrackedState(stateDir string) (*trackedState, error) {
	trackedPath := m.trackedStatePath(stateDir)
	b, err := os.ReadFile(trackedPath)
	if err != nil {
		return nil, fmt.Errorf("read tracked state %s: %w", trackedPath, err)
	}
	var state trackedState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("parse tracked state %s: %w", trackedPath, err)
	}
	if state.Version == 0 {
		return nil, fmt.Errorf("tracked state %s is missing version", trackedPath)
	}
	return &state, nil
}

func (m *Manager) loadTrackedStateForInstall(stateDir string) (*trackedState, error) {
	state, err := m.loadTrackedState(stateDir)
	if err == nil {
		return state, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("tracked state is unreadable; resolve %s and retry: %w", m.trackedStatePath(stateDir), err)
}

func (m *Manager) saveTrackedState(stateDir string, state trackedState) error {
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
	if err := tmp.Chmod(stateFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod tracked temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tracked temp file: %w", err)
	}

	if err := os.Rename(tmpPath, m.trackedStatePath(stateDir)); err != nil {
		return fmt.Errorf("replace tracked state: %w", err)
	}
	return nil
}
