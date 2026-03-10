package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func readJSONMap(path string) (map[string]any, error) {
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

func writeJSONMap(path string, v map[string]any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, stateFilePerm)
}

func mustMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", name, value)
	}
	return m
}

func mustString(t *testing.T, value any, name string) string {
	t.Helper()
	s, ok := value.(string)
	if !ok {
		t.Fatalf("%s type = %T, want string", name, value)
	}
	return s
}

func mustFloat64(t *testing.T, value any, name string) float64 {
	t.Helper()
	n, ok := value.(float64)
	if !ok {
		t.Fatalf("%s type = %T, want float64", name, value)
	}
	return n
}
