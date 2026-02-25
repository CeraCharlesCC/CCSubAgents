package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/versiontag"
)

type InstallSettings struct {
	AutostartWebUI bool
	PinnedVersion  string
}

type settingsPatch struct {
	HasAutostartWebUI bool
	AutostartWebUI    bool
	HasPinnedVersion  bool
	PinnedVersionRaw  string
}

type SettingsScope string

const (
	SettingsScopeGlobal SettingsScope = "global"
	SettingsScopeLocal  SettingsScope = "local"
)

func ResolveSettingsPaths(home, cwd string) (string, string) {
	globalPath := filepath.Join(home, ".local", "share", "ccsubagents", "settings.json")
	cleanCwd := filepath.Clean(cwd)
	localRoot := cleanCwd
	if !strings.EqualFold(filepath.Base(cleanCwd), "ccsubagents") {
		localRoot = filepath.Join(cleanCwd, "ccsubagents")
	}
	localPath := filepath.Join(localRoot, "settings.json")
	return globalPath, localPath
}

func NormalizeVersionTag(raw string) string {
	return versiontag.Normalize(raw)
}

func NormalizeInstallVersionTag(raw string) string {
	return NormalizeVersionTag(raw)
}

func isPathMissingError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not a directory")
}

func readSettingsPatch(path string) (settingsPatch, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if isPathMissingError(err) {
			return settingsPatch{}, nil
		}
		return settingsPatch{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return settingsPatch{}, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		return settingsPatch{}, err
	}
	if root == nil {
		return settingsPatch{}, nil
	}

	var patch settingsPatch
	if raw, ok := root["autostart-webui"]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return settingsPatch{}, fmt.Errorf("key autostart-webui must be a boolean")
		}
		patch.HasAutostartWebUI = true
		patch.AutostartWebUI = enabled
	}

	if raw, ok := root["pinned-version"]; ok {
		patch.HasPinnedVersion = true
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			patch.PinnedVersionRaw = ""
		} else {
			var pinned string
			if err := json.Unmarshal(raw, &pinned); err != nil {
				return settingsPatch{}, fmt.Errorf("key pinned-version must be a string or null")
			}
			patch.PinnedVersionRaw = NormalizeVersionTag(pinned)
		}
	}

	return patch, nil
}

func mergeSettings(globalPatch, localPatch settingsPatch) InstallSettings {
	settings := InstallSettings{}
	applyPatch := func(patch settingsPatch) {
		if patch.HasAutostartWebUI {
			settings.AutostartWebUI = patch.AutostartWebUI
		}
		if patch.HasPinnedVersion {
			settings.PinnedVersion = patch.PinnedVersionRaw
		}
	}

	applyPatch(globalPatch)
	applyPatch(localPatch)
	return settings
}

func LoadMergedInstallSettings(home, cwd string) (InstallSettings, error) {
	globalPath, localPath := ResolveSettingsPaths(home, cwd)

	globalPatch, err := readSettingsPatch(globalPath)
	if err != nil {
		return InstallSettings{}, fmt.Errorf("read settings file %s: %w", globalPath, err)
	}

	localPatch, err := readSettingsPatch(localPath)
	if err != nil {
		return InstallSettings{}, fmt.Errorf("read settings file %s: %w", localPath, err)
	}

	return mergeSettings(globalPatch, localPatch), nil
}

func ChoosePinWritePath(cwd, home string) (string, SettingsScope, error) {
	globalPath, localPath := ResolveSettingsPaths(home, cwd)
	localDir := filepath.Dir(localPath)
	info, err := os.Stat(localDir)
	if err == nil {
		if info.IsDir() {
			return localPath, SettingsScopeLocal, nil
		}
		return globalPath, SettingsScopeGlobal, nil
	}
	if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("check local settings directory %s: %w", localDir, err)
	}

	return globalPath, SettingsScopeGlobal, nil
}

func WritePinnedVersion(path, versionTag string, dirPerm, filePerm os.FileMode) error {
	normalized := NormalizeVersionTag(versionTag)
	if normalized == "" {
		return errors.New("pinned version cannot be empty")
	}

	root, err := readJSONFile(path)
	if err != nil {
		return fmt.Errorf("read settings file %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create settings directory for %s: %w", path, err)
	}

	root["pinned-version"] = normalized
	if err := writeJSONFile(path, root, filePerm); err != nil {
		return fmt.Errorf("write settings file %s: %w", path, err)
	}

	return nil
}
