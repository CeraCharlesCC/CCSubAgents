package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

func applySettingsEdit(settingsPath, agentsDir string, previous *trackedState) (settingsEdit, error) {
	root, err := readJSONFile(settingsPath)
	if err != nil {
		return settingsEdit{}, fmt.Errorf("read settings.json: %w", err)
	}

	added := false
	if current, exists := root[settingsAgentPathKey]; exists {
		locations, ok := current.(map[string]any)
		if !ok {
			return settingsEdit{}, fmt.Errorf("settings %s must be an object when present", settingsAgentPathKey)
		}
		if previous != nil {
			previousPath := strings.TrimSpace(previous.JSONEdits.Settings.AgentPath)
			if previousPath != "" && previousPath != agentsDir {
				delete(locations, previousPath)
			}
		}
		if _, exists := locations[agentsDir]; !exists {
			locations[agentsDir] = true
			added = true
		}
	} else {
		root[settingsAgentPathKey] = map[string]any{agentsDir: true}
		added = true
	}

	if chatRaw, chatExists := root["chat"]; chatExists {
		if _, ok := chatRaw.(map[string]any); !ok {
			return settingsEdit{}, errors.New("settings key chat must be an object when present")
		}
	}

	if err := writeJSONFile(settingsPath, root); err != nil {
		return settingsEdit{}, fmt.Errorf("write settings.json: %w", err)
	}

	wasAdded := added
	if previous != nil && previous.JSONEdits.Settings.Added {
		wasAdded = true
	}
	return settingsEdit{File: settingsPath, AgentPath: agentsDir, Added: wasAdded}, nil
}

func revertSettingsEdit(edit settingsEdit) error {
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

	locationsRaw, ok := root[settingsAgentPathKey]
	if !ok {
		return nil
	}
	locations, ok := locationsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("settings %s must be an object when present", settingsAgentPathKey)
	}
	if _, exists := locations[edit.AgentPath]; !exists {
		return nil
	}
	delete(locations, edit.AgentPath)

	if err := writeJSONFile(edit.File, root); err != nil {
		return fmt.Errorf("write settings.json during uninstall: %w", err)
	}
	return nil
}

func applyMCPEdit(path, commandPath string, previous *trackedState) (mcpEdit, error) {
	root, err := readJSONFile(path)
	if err != nil {
		return mcpEdit{}, fmt.Errorf("read mcp.json: %w", err)
	}

	servers, err := ensureObject(root, "servers")
	if err != nil {
		return mcpEdit{}, fmt.Errorf("mcp key servers: %w", err)
	}

	edit := mcpEdit{File: path, Key: mcpServerKey, Touched: true}
	if prev := previous; prev != nil && prev.JSONEdits.MCP.Touched {
		edit.HadPrevious = prev.JSONEdits.MCP.HadPrevious
		if len(prev.JSONEdits.MCP.Previous) > 0 {
			edit.Previous = slices.Clone(prev.JSONEdits.MCP.Previous)
		}
	} else if existing, ok := servers[mcpServerKey]; ok {
		encoded, err := json.Marshal(existing)
		if err != nil {
			return mcpEdit{}, fmt.Errorf("marshal existing mcp server config: %w", err)
		}
		edit.HadPrevious = true
		edit.Previous = encoded
	}

	servers[mcpServerKey] = map[string]any{
		"command": commandPath,
	}

	if err := writeJSONFile(path, root); err != nil {
		return mcpEdit{}, fmt.Errorf("write mcp.json: %w", err)
	}

	return edit, nil
}

func revertMCPEdit(edit mcpEdit) error {
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

	if err := writeJSONFile(edit.File, root); err != nil {
		return fmt.Errorf("write mcp.json during uninstall: %w", err)
	}
	return nil
}

func readJSONFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func writeJSONFile(path string, v map[string]any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, stateFilePerm)
}

func ensureObject(root map[string]any, key string) (map[string]any, error) {
	v, exists := root[key]
	if !exists {
		next := map[string]any{}
		root[key] = next
		return next, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object when present", key)
	}
	return obj, nil
}
