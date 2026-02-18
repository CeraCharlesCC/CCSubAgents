package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	Settings      settingsEdit   `json:"settings"`
	SettingsExtra []settingsEdit `json:"settingsExtra,omitempty"`
	MCP           mcpEdit        `json:"mcp"`
	MCPExtra      []mcpEdit      `json:"mcpExtra,omitempty"`
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

func trackedJSONOpsFromEdits(settings []settingsEdit, mcp []mcpEdit) trackedJSONOps {
	out := trackedJSONOps{}
	if len(settings) > 0 {
		out.Settings = settings[0]
		if len(settings) > 1 {
			out.SettingsExtra = slices.Clone(settings[1:])
		}
	}
	if len(mcp) > 0 {
		out.MCP = mcp[0]
		if len(mcp) > 1 {
			out.MCPExtra = slices.Clone(mcp[1:])
		}
	}
	return out
}

func (ops trackedJSONOps) allSettingsEdits() []settingsEdit {
	out := make([]settingsEdit, 0, 1+len(ops.SettingsExtra))
	if stringsHasValue(ops.Settings.File) || stringsHasValue(ops.Settings.AgentPath) || ops.Settings.Added {
		out = append(out, ops.Settings)
	}
	out = append(out, ops.SettingsExtra...)
	return out
}

func (ops trackedJSONOps) allMCPEdits() []mcpEdit {
	out := make([]mcpEdit, 0, 1+len(ops.MCPExtra))
	if stringsHasValue(ops.MCP.File) || stringsHasValue(ops.MCP.Key) || ops.MCP.Touched || ops.MCP.HadPrevious || len(ops.MCP.Previous) > 0 {
		out = append(out, ops.MCP)
	}
	out = append(out, ops.MCPExtra...)
	return out
}

func (ops trackedJSONOps) settingsEditForFile(path string) (settingsEdit, bool) {
	cleanPath := filepath.Clean(path)
	for _, edit := range ops.allSettingsEdits() {
		if filepath.Clean(edit.File) == cleanPath {
			return edit, true
		}
	}
	return settingsEdit{}, false
}

func (ops trackedJSONOps) mcpEditForFile(path string) (mcpEdit, bool) {
	cleanPath := filepath.Clean(path)
	for _, edit := range ops.allMCPEdits() {
		if filepath.Clean(edit.File) == cleanPath {
			return edit, true
		}
	}
	return mcpEdit{}, false
}

func stringsHasValue(value string) bool {
	return strings.TrimSpace(value) != ""
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
