package state

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

const (
	TrackedSchemaVersion = 3
	TrackedFileName      = "tracked.json"
)

type AppliedStep struct {
	ID         string         `json:"id"`
	InputsHash string         `json:"inputsHash"`
	Outputs    map[string]any `json:"outputs,omitempty"`
	AppliedAt  string         `json:"appliedAt"`
}

type TrackedState struct {
	Version      int            `json:"version"`
	Repo         string         `json:"repo"`
	ReleaseID    int64          `json:"releaseId"`
	ReleaseTag   string         `json:"releaseTag"`
	InstalledAt  string         `json:"installedAt"`
	Managed      ManagedState   `json:"managed"`
	AppliedSteps []AppliedStep  `json:"appliedSteps,omitempty"`
	JSONEdits    TrackedJSONOps `json:"jsonEdits"`
	Local        []LocalInstall `json:"local,omitempty"`
}

type LocalInstallMode string

const (
	LocalInstallModePersonal LocalInstallMode = "personal"
	LocalInstallModeTeam     LocalInstallMode = "team"
)

type LocalInstall struct {
	InstallRoot  string           `json:"installRoot"`
	Mode         LocalInstallMode `json:"mode,omitempty"`
	BinaryOnly   bool             `json:"binaryOnly,omitempty"`
	Repo         string           `json:"repo"`
	ReleaseID    int64            `json:"releaseId"`
	ReleaseTag   string           `json:"releaseTag"`
	InstalledAt  string           `json:"installedAt"`
	Managed      ManagedState     `json:"managed"`
	AppliedSteps []AppliedStep    `json:"appliedSteps,omitempty"`
	JSONEdits    TrackedJSONOps   `json:"jsonEdits"`
	IgnoreEdits  []IgnoreEdit     `json:"ignoreEdits,omitempty"`
}

type IgnoreEdit struct {
	File       string   `json:"file"`
	AddedLines []string `json:"addedLines,omitempty"`
}

type ManagedState struct {
	Files []string `json:"files"`
	Dirs  []string `json:"dirs"`
}

type TrackedJSONOps struct {
	Settings      SettingsEdit   `json:"settings"`
	SettingsExtra []SettingsEdit `json:"settingsExtra,omitempty"`
	MCP           MCPEdit        `json:"mcp"`
	MCPExtra      []MCPEdit      `json:"mcpExtra,omitempty"`
}

type SettingsEdit struct {
	File      string `json:"file"`
	AgentPath string `json:"agentPath"`
	Mode      string `json:"mode,omitempty"`
	Added     bool   `json:"added"`
}

type MCPEdit struct {
	File        string          `json:"file"`
	Key         string          `json:"key"`
	Touched     bool            `json:"touched"`
	HadPrevious bool            `json:"hadPrevious"`
	Previous    json.RawMessage `json:"previous,omitempty"`
}

func cloneMCPEdit(edit MCPEdit) MCPEdit {
	out := edit
	if len(edit.Previous) > 0 {
		out.Previous = slices.Clone(edit.Previous)
	}
	return out
}

func (ops TrackedJSONOps) Clone() TrackedJSONOps {
	out := TrackedJSONOps{
		Settings: ops.Settings,
		MCP:      cloneMCPEdit(ops.MCP),
	}
	if len(ops.SettingsExtra) > 0 {
		out.SettingsExtra = slices.Clone(ops.SettingsExtra)
	}
	if len(ops.MCPExtra) > 0 {
		out.MCPExtra = make([]MCPEdit, 0, len(ops.MCPExtra))
		for _, edit := range ops.MCPExtra {
			out.MCPExtra = append(out.MCPExtra, cloneMCPEdit(edit))
		}
	}
	return out
}

func TrackedJSONOpsFromEdits(settings []SettingsEdit, mcp []MCPEdit) TrackedJSONOps {
	out := TrackedJSONOps{}
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

func (ops TrackedJSONOps) AllSettingsEdits() []SettingsEdit {
	out := make([]SettingsEdit, 0, 1+len(ops.SettingsExtra))
	if stringsHasValue(ops.Settings.File) || stringsHasValue(ops.Settings.AgentPath) || ops.Settings.Added {
		out = append(out, ops.Settings)
	}
	out = append(out, ops.SettingsExtra...)
	return out
}

func (ops TrackedJSONOps) AllMCPEdits() []MCPEdit {
	out := make([]MCPEdit, 0, 1+len(ops.MCPExtra))
	if stringsHasValue(ops.MCP.File) || stringsHasValue(ops.MCP.Key) || ops.MCP.Touched || ops.MCP.HadPrevious || len(ops.MCP.Previous) > 0 {
		out = append(out, ops.MCP)
	}
	out = append(out, ops.MCPExtra...)
	return out
}

func (ops TrackedJSONOps) SettingsEditForFile(path string) (SettingsEdit, bool) {
	cleanPath := filepath.Clean(path)
	for _, edit := range ops.AllSettingsEdits() {
		if filepath.Clean(edit.File) == cleanPath {
			return edit, true
		}
	}
	return SettingsEdit{}, false
}

func (ops TrackedJSONOps) MCPEditForFile(path string) (MCPEdit, bool) {
	cleanPath := filepath.Clean(path)
	for _, edit := range ops.AllMCPEdits() {
		if filepath.Clean(edit.File) == cleanPath {
			return edit, true
		}
	}
	return MCPEdit{}, false
}

func stringsHasValue(value string) bool {
	return strings.TrimSpace(value) != ""
}

func (state *TrackedState) HasGlobalInstall() bool {
	if state == nil {
		return false
	}
	if stringsHasValue(state.Repo) || stringsHasValue(state.ReleaseTag) || stringsHasValue(state.InstalledAt) || state.ReleaseID != 0 {
		return true
	}
	if len(state.Managed.Files) > 0 || len(state.Managed.Dirs) > 0 {
		return true
	}
	if len(state.JSONEdits.AllSettingsEdits()) > 0 || len(state.JSONEdits.AllMCPEdits()) > 0 {
		return true
	}
	return false
}

func (state *TrackedState) GlobalInstallSnapshot() *TrackedState {
	if state == nil || !state.HasGlobalInstall() {
		return nil
	}
	return &TrackedState{
		Version:     state.Version,
		Repo:        state.Repo,
		ReleaseID:   state.ReleaseID,
		ReleaseTag:  state.ReleaseTag,
		InstalledAt: state.InstalledAt,
		Managed: ManagedState{
			Files: slices.Clone(state.Managed.Files),
			Dirs:  slices.Clone(state.Managed.Dirs),
		},
		AppliedSteps: slices.Clone(state.AppliedSteps),
		JSONEdits:    state.JSONEdits.Clone(),
	}
}

func (state *TrackedState) LocalInstallForRoot(root string) (*LocalInstall, int) {
	if state == nil {
		return nil, -1
	}
	cleanRoot := filepath.Clean(root)
	for idx := range state.Local {
		if filepath.Clean(state.Local[idx].InstallRoot) == cleanRoot {
			return &state.Local[idx], idx
		}
	}
	return nil, -1
}

func (state *TrackedState) SetLocalInstall(next LocalInstall) {
	if state == nil {
		return
	}
	next.InstallRoot = filepath.Clean(next.InstallRoot)
	if existing, idx := state.LocalInstallForRoot(next.InstallRoot); idx >= 0 && existing != nil {
		state.Local[idx] = next
		return
	}
	state.Local = append(state.Local, next)
}

func (state *TrackedState) RemoveLocalInstall(root string) {
	if state == nil {
		return
	}
	_, idx := state.LocalInstallForRoot(root)
	if idx < 0 {
		return
	}
	state.Local = append(state.Local[:idx], state.Local[idx+1:]...)
}

func (state *TrackedState) ClearGlobalInstall() {
	if state == nil {
		return
	}
	state.Repo = ""
	state.ReleaseID = 0
	state.ReleaseTag = ""
	state.InstalledAt = ""
	state.Managed = ManagedState{}
	state.AppliedSteps = nil
	state.JSONEdits = TrackedJSONOps{}
}

func (state *TrackedState) Empty() bool {
	if state == nil {
		return true
	}
	return !state.HasGlobalInstall() && len(state.Local) == 0
}
