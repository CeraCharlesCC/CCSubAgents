package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type installSettings struct {
	AutostartWebUI bool
	PinnedVersion  string
}

type settingsPatch struct {
	HasAutostartWebUI bool
	AutostartWebUI    bool
	HasPinnedVersion  bool
	PinnedVersionRaw  string
}

type settingsScope string

const (
	settingsScopeGlobal settingsScope = "global"
	settingsScopeLocal  settingsScope = "local"
)

func resolveSettingsPaths(home, cwd string) (string, string) {
	globalPath := filepath.Join(home, ".local", "share", "ccsubagents", "settings.json")
	localPath := filepath.Join(cwd, "ccsubagents", "settings.json")
	return globalPath, localPath
}

func normalizeVersionTag(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "none") {
		return ""
	}
	if strings.EqualFold(trimmed, "null") {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "V") {
		return "v" + strings.TrimPrefix(trimmed, "V")
	}
	return "v" + trimmed
}

func NormalizeInstallVersionTag(raw string) string {
	return normalizeVersionTag(raw)
}

func readSettingsPatch(path string) (settingsPatch, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
			patch.PinnedVersionRaw = normalizeVersionTag(pinned)
		}
	}

	return patch, nil
}

func mergeSettings(globalPatch, localPatch settingsPatch) installSettings {
	settings := installSettings{}
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

func loadMergedInstallSettings(home, cwd string) (installSettings, error) {
	globalPath, localPath := resolveSettingsPaths(home, cwd)

	globalPatch, err := readSettingsPatch(globalPath)
	if err != nil {
		return installSettings{}, fmt.Errorf("read settings file %s: %w", globalPath, err)
	}

	localPatch, err := readSettingsPatch(localPath)
	if err != nil {
		return installSettings{}, fmt.Errorf("read settings file %s: %w", localPath, err)
	}

	return mergeSettings(globalPatch, localPatch), nil
}

func choosePinWritePath(cwd, home string) (string, settingsScope, error) {
	globalPath, localPath := resolveSettingsPaths(home, cwd)
	localDir := filepath.Dir(localPath)
	info, err := os.Stat(localDir)
	if err == nil {
		if info.IsDir() {
			return localPath, settingsScopeLocal, nil
		}
		return "", "", fmt.Errorf("local settings parent exists but is not a directory: %s", localDir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("check local settings directory %s: %w", localDir, err)
	}

	return globalPath, settingsScopeGlobal, nil
}

func writePinnedVersion(path, versionTag string) error {
	normalized := normalizeVersionTag(versionTag)
	if normalized == "" {
		return errors.New("pinned version cannot be empty")
	}

	root, err := readJSONFile(path)
	if err != nil {
		return fmt.Errorf("read settings file %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), stateDirPerm); err != nil {
		return fmt.Errorf("create settings directory for %s: %w", path, err)
	}

	root["pinned-version"] = normalized
	if err := writeJSONFile(path, root); err != nil {
		return fmt.Errorf("write settings file %s: %w", path, err)
	}

	return nil
}
