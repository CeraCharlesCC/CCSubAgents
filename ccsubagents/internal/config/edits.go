package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

const (
	MCPServerKey         = "artifact-mcp"
	SettingsAgentPathKey = "chat.agentFilesLocations"
)

func ApplySettingsEdit(settingsPath, agentsDir string, previous *state.TrackedState, filePerm os.FileMode) (state.SettingsEdit, error) {
	var previousEdit state.SettingsEdit
	hasPreviousEdit := false
	if previous != nil {
		if matched, ok := previous.JSONEdits.SettingsEditForFile(settingsPath); ok {
			previousEdit = matched
			hasPreviousEdit = true
		} else {
			all := previous.JSONEdits.AllSettingsEdits()
			if len(all) == 1 && strings.TrimSpace(all[0].File) == "" {
				previousEdit = all[0]
				hasPreviousEdit = true
			}
		}
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		return state.SettingsEdit{}, fmt.Errorf("read settings.json: %w", err)
	}

	added := false
	if current, exists := root[SettingsAgentPathKey]; exists {
		locations, ok := current.(map[string]any)
		if !ok {
			return state.SettingsEdit{}, fmt.Errorf("settings %s must be an object when present", SettingsAgentPathKey)
		}
		if hasPreviousEdit {
			previousPath := strings.TrimSpace(previousEdit.AgentPath)
			if previousPath != "" && previousPath != agentsDir {
				delete(locations, previousPath)
			}
		}
		if _, exists := locations[agentsDir]; !exists {
			locations[agentsDir] = true
			added = true
		}
	} else {
		root[SettingsAgentPathKey] = map[string]any{agentsDir: true}
		added = true
	}

	if chatRaw, chatExists := root["chat"]; chatExists {
		if _, ok := chatRaw.(map[string]any); !ok {
			return state.SettingsEdit{}, errors.New("settings key chat must be an object when present")
		}
	}

	if err := writeJSONFile(settingsPath, root, filePerm); err != nil {
		return state.SettingsEdit{}, fmt.Errorf("write settings.json: %w", err)
	}

	wasAdded := added
	if hasPreviousEdit && previousEdit.Added {
		wasAdded = true
	}
	return state.SettingsEdit{File: settingsPath, AgentPath: agentsDir, Added: wasAdded}, nil
}

func RevertSettingsEdit(edit state.SettingsEdit, filePerm os.FileMode) error {
	if !edit.Added {
		return nil
	}
	root, err := readJSONFile(edit.File)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read settings.json for uninstall: %w", err)
	}

	locationsRaw, ok := root[SettingsAgentPathKey]
	if !ok {
		return nil
	}
	locations, ok := locationsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("settings %s must be an object when present", SettingsAgentPathKey)
	}
	if _, exists := locations[edit.AgentPath]; !exists {
		return nil
	}
	delete(locations, edit.AgentPath)

	if err := writeJSONFile(edit.File, root, filePerm); err != nil {
		return fmt.Errorf("write settings.json during uninstall: %w", err)
	}
	return nil
}

func ApplyMCPEdit(path, commandPath string, previous *state.TrackedState, filePerm os.FileMode) (state.MCPEdit, error) {
	root, err := readJSONFile(path)
	if err != nil {
		return state.MCPEdit{}, fmt.Errorf("read mcp.json: %w", err)
	}

	servers, err := ensureObject(root, "servers")
	if err != nil {
		return state.MCPEdit{}, fmt.Errorf("mcp key servers: %w", err)
	}

	edit := state.MCPEdit{File: path, Key: MCPServerKey, Touched: true}
	if prev := previous; prev != nil {
		if matched, ok := prev.JSONEdits.MCPEditForFile(path); ok && matched.Touched {
			edit.HadPrevious = matched.HadPrevious
			if len(matched.Previous) > 0 {
				edit.Previous = slices.Clone(matched.Previous)
			}
		} else {
			all := prev.JSONEdits.AllMCPEdits()
			if len(all) == 1 && all[0].Touched && strings.TrimSpace(all[0].File) == "" {
				edit.HadPrevious = all[0].HadPrevious
				if len(all[0].Previous) > 0 {
					edit.Previous = slices.Clone(all[0].Previous)
				}
			} else if existing, ok := servers[MCPServerKey]; ok {
				encoded, err := json.Marshal(existing)
				if err != nil {
					return state.MCPEdit{}, fmt.Errorf("marshal existing mcp server config: %w", err)
				}
				edit.HadPrevious = true
				edit.Previous = encoded
			}
		}
	} else if existing, ok := servers[MCPServerKey]; ok {
		encoded, err := json.Marshal(existing)
		if err != nil {
			return state.MCPEdit{}, fmt.Errorf("marshal existing mcp server config: %w", err)
		}
		edit.HadPrevious = true
		edit.Previous = encoded
	}

	servers[MCPServerKey] = map[string]any{
		"command": commandPath,
	}

	if err := writeJSONFile(path, root, filePerm); err != nil {
		return state.MCPEdit{}, fmt.Errorf("write mcp.json: %w", err)
	}

	return edit, nil
}

func RevertMCPEdit(edit state.MCPEdit, filePerm os.FileMode) error {
	if !edit.Touched {
		return nil
	}
	root, err := readJSONFile(edit.File)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read mcp.json for uninstall: %w", err)
	}

	servers, err := ensureObject(root, "servers")
	if err != nil {
		return fmt.Errorf("mcp key servers: %w", err)
	}

	if edit.HadPrevious {
		if len(edit.Previous) == 0 {
			return errors.New("tracked mcp previous value is missing")
		}
		var restored any
		if err := json.Unmarshal(edit.Previous, &restored); err != nil {
			return fmt.Errorf("decode tracked previous mcp value: %w", err)
		}
		servers[edit.Key] = restored
	} else {
		delete(servers, edit.Key)
	}

	if err := writeJSONFile(edit.File, root, filePerm); err != nil {
		return fmt.Errorf("write mcp.json during uninstall: %w", err)
	}
	return nil
}
