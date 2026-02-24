package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

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

func writeJSONFile(path string, v map[string]any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, perm)
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
