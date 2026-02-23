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

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previous)
	})
}

func TestResolveAutostartWebUI_LocalOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDirectory(t, cwd)

	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, globalPath, `{"autostart-webui": true}`)
	writeSettingsFile(t, localPath, `{"autostart-webui": false}`)

	enabled, err := ResolveAutostartWebUI()
	if err != nil {
		t.Fatalf("ResolveAutostartWebUI returned error: %v", err)
	}
	if enabled {
		t.Fatalf("expected local override to disable autostart-webui")
	}
}

func TestResolveAutostartWebUI_LocalEnablesWhenGlobalMissing(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDirectory(t, cwd)

	_, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, localPath, `{"autostart-webui": true}`)

	enabled, err := ResolveAutostartWebUI()
	if err != nil {
		t.Fatalf("ResolveAutostartWebUI returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected local settings to enable autostart-webui")
	}
}

func TestResolveAutostartWebUI_MissingFilesDefaultFalse(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDirectory(t, cwd)

	enabled, err := ResolveAutostartWebUI()
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
	t.Setenv("HOME", home)
	withWorkingDirectory(t, cwd)

	globalPath, _ := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, globalPath, `{"autostart-webui": "yes"}`)

	_, err := ResolveAutostartWebUI()
	if err == nil {
		t.Fatalf("expected type mismatch error")
	}
	if !strings.Contains(err.Error(), "autostart-webui") {
		t.Fatalf("expected autostart-webui type error, got %v", err)
	}
}
