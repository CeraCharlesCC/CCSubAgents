package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ccsubagentsConfigDirEnv = "CCSUBAGENTS_CONFIG_DIR"

type CCSubagentsSettings struct {
	AutostartWebUI bool
	NoAuth         bool
	WebUIPort      int
}

type ccsubagentsSettingsPatch struct {
	HasAutostartWebUI bool
	AutostartWebUI    bool
	HasNoAuth         bool
	NoAuth            bool
	HasWebUIPort      bool
	WebUIPort         int
}

func ResolveCCSubagentsSettings() (CCSubagentsSettings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return CCSubagentsSettings{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return CCSubagentsSettings{}, err
	}

	return resolveMergedCCSubagentsSettings(home, cwd)
}

func ResolveAutostartWebUI() (bool, error) {
	settings, err := ResolveCCSubagentsSettings()
	if err != nil {
		return false, err
	}
	return settings.AutostartWebUI, nil
}

func resolveCCSubagentsSettingsPaths(home, cwd string) (globalPath, localPath string) {
	globalConfigDir := filepath.Join(defaultGlobalBase(home), "config")
	if configOverride := strings.TrimSpace(os.Getenv(ccsubagentsConfigDirEnv)); configOverride != "" {
		globalConfigDir = filepath.Clean(configOverride)
	}

	workspaceRoot := resolveWorkspaceRoot(cwd)
	globalPath = filepath.Join(globalConfigDir, "settings.json")
	localPath = filepath.Join(workspaceRoot, "settings.json")
	return globalPath, localPath
}

func resolveWorkspaceRoot(cwd string) string {
	cleanCwd := filepath.Clean(strings.TrimSpace(cwd))
	if cleanCwd == "" || cleanCwd == "." {
		cleanCwd = "."
	}
	if strings.EqualFold(filepath.Base(cleanCwd), "ccsubagents") {
		return cleanCwd
	}
	return filepath.Join(cleanCwd, "ccsubagents")
}

func resolveMergedCCSubagentsSettings(home, cwd string) (CCSubagentsSettings, error) {
	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)

	globalPatch, err := readCCSubagentsSettingsPatch(globalPath)
	if err != nil {
		return CCSubagentsSettings{}, fmt.Errorf("read settings file %s: %w", globalPath, err)
	}

	localPatch, err := readCCSubagentsSettingsPatch(localPath)
	if err != nil {
		return CCSubagentsSettings{}, fmt.Errorf("read settings file %s: %w", localPath, err)
	}

	settings := CCSubagentsSettings{}
	applyPatch := func(patch ccsubagentsSettingsPatch) {
		if patch.HasAutostartWebUI {
			settings.AutostartWebUI = patch.AutostartWebUI
		}
		if patch.HasNoAuth {
			settings.NoAuth = patch.NoAuth
		}
		if patch.HasWebUIPort {
			settings.WebUIPort = patch.WebUIPort
		}
	}

	applyPatch(globalPatch)
	applyPatch(localPatch)
	return settings, nil
}

func isPathMissingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not a directory")
}

func readCCSubagentsSettingsPatch(path string) (ccsubagentsSettingsPatch, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if isPathMissingError(err) {
			return ccsubagentsSettingsPatch{}, nil
		}
		return ccsubagentsSettingsPatch{}, err
	}

	if len(bytes.TrimSpace(b)) == 0 {
		return ccsubagentsSettingsPatch{}, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		return ccsubagentsSettingsPatch{}, err
	}
	if root == nil {
		return ccsubagentsSettingsPatch{}, nil
	}

	var patch ccsubagentsSettingsPatch

	if raw, ok := root["autostart-webui"]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return ccsubagentsSettingsPatch{}, fmt.Errorf("key autostart-webui must be a boolean")
		}
		patch.HasAutostartWebUI = true
		patch.AutostartWebUI = enabled
	}

	if raw, ok := root["no-auth"]; ok {
		var noAuth bool
		if err := json.Unmarshal(raw, &noAuth); err != nil {
			return ccsubagentsSettingsPatch{}, fmt.Errorf("key no-auth must be a boolean")
		}
		patch.HasNoAuth = true
		patch.NoAuth = noAuth
	}

	if raw, ok := root["webui-port"]; ok {
		var port int
		if err := json.Unmarshal(raw, &port); err != nil {
			return ccsubagentsSettingsPatch{}, fmt.Errorf("key webui-port must be an integer between 1 and 65535")
		}
		if port < 1 || port > 65535 {
			return ccsubagentsSettingsPatch{}, fmt.Errorf("key webui-port must be between 1 and 65535")
		}
		patch.HasWebUIPort = true
		patch.WebUIPort = port
	}

	return patch, nil
}
