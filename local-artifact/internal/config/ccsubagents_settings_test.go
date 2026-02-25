package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettingsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}
}

func TestResolveAutostartWebUI_LocalOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, globalPath, `{"autostart-webui": true}`)
	writeSettingsFile(t, localPath, `{"autostart-webui": false}`)

	enabled, err := resolveMergedAutostartWebUI(home, cwd)
	if err != nil {
		t.Fatalf("resolveMergedAutostartWebUI returned error: %v", err)
	}
	if enabled {
		t.Fatalf("expected local override to disable autostart-webui")
	}
}

func TestResolveAutostartWebUI_LocalEnablesWhenGlobalMissing(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	_, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, localPath, `{"autostart-webui": true}`)

	enabled, err := resolveMergedAutostartWebUI(home, cwd)
	if err != nil {
		t.Fatalf("resolveMergedAutostartWebUI returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected local settings to enable autostart-webui")
	}
}

func TestResolveAutostartWebUI_MissingFilesDefaultFalse(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	enabled, err := resolveMergedAutostartWebUI(home, cwd)
	if err != nil {
		t.Fatalf("expected missing files to be treated as empty settings, got %v", err)
	}
	if enabled {
		t.Fatalf("expected default autostart-webui to be false")
	}
}

func TestResolveAutostartWebUI_TypeMismatchFails(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	globalPath, _ := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, globalPath, `{"autostart-webui": "yes"}`)

	_, err := resolveMergedAutostartWebUI(home, cwd)
	if err == nil {
		t.Fatalf("expected type mismatch error")
	}
	if !strings.Contains(err.Error(), "autostart-webui") {
		t.Fatalf("expected autostart-webui type error, got %v", err)
	}
}
