package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type autostartPatch struct {
	HasAutostartWebUI bool
	AutostartWebUI    bool
}

func ResolveAutostartWebUI() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}

	return resolveMergedAutostartWebUI(home, cwd)
}

func resolveCCSubagentsSettingsPaths(home, cwd string) (globalPath, localPath string) {
	globalPath = filepath.Join(home, ".local", "share", "ccsubagents", "settings.json")
	localPath = filepath.Join(cwd, "ccsubagents", "settings.json")
	return globalPath, localPath
}

func resolveMergedAutostartWebUI(home, cwd string) (bool, error) {
	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)

	globalPatch, err := readAutostartPatch(globalPath)
	if err != nil {
		return false, fmt.Errorf("read settings file %s: %w", globalPath, err)
	}

	localPatch, err := readAutostartPatch(localPath)
	if err != nil {
		return false, fmt.Errorf("read settings file %s: %w", localPath, err)
	}

	enabled := false
	if globalPatch.HasAutostartWebUI {
		enabled = globalPatch.AutostartWebUI
	}
	if localPatch.HasAutostartWebUI {
		enabled = localPatch.AutostartWebUI
	}

	return enabled, nil
}

func readAutostartPatch(path string) (autostartPatch, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return autostartPatch{}, nil
		}
		return autostartPatch{}, err
	}

	if len(bytes.TrimSpace(b)) == 0 {
		return autostartPatch{}, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		return autostartPatch{}, err
	}
	if root == nil {
		return autostartPatch{}, nil
	}

	raw, ok := root["autostart-webui"]
	if !ok {
		return autostartPatch{}, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return autostartPatch{}, fmt.Errorf("key autostart-webui must be a boolean")
	}

	return autostartPatch{HasAutostartWebUI: true, AutostartWebUI: enabled}, nil
}
