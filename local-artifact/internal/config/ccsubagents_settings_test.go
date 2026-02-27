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

func TestResolveMergedCCSubagentsSettings_LocalOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	writeSettingsFile(t, globalPath, `{"autostart-webui": true, "no-auth": false, "webui-port": 19131}`)
	writeSettingsFile(t, localPath, `{"no-auth": true, "webui-port": 19132}`)

	settings, err := resolveMergedCCSubagentsSettings(home, cwd)
	if err != nil {
		t.Fatalf("resolveMergedCCSubagentsSettings returned error: %v", err)
	}
	if !settings.AutostartWebUI {
		t.Fatalf("expected autostart-webui to remain true from global settings")
	}
	if !settings.NoAuth {
		t.Fatalf("expected local override to enable no-auth")
	}
	if settings.WebUIPort != 19132 {
		t.Fatalf("webui-port mismatch: got=%d want=%d", settings.WebUIPort, 19132)
	}
}

func TestResolveMergedCCSubagentsSettings_MissingFilesDefaults(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	settings, err := resolveMergedCCSubagentsSettings(home, cwd)
	if err != nil {
		t.Fatalf("resolveMergedCCSubagentsSettings returned error: %v", err)
	}
	if settings.AutostartWebUI {
		t.Fatalf("expected default autostart-webui to be false")
	}
	if settings.NoAuth {
		t.Fatalf("expected default no-auth to be false")
	}
	if settings.WebUIPort != 0 {
		t.Fatalf("expected default webui-port to be unset (0), got %d", settings.WebUIPort)
	}
}

func TestResolveCCSubagentsSettingsPaths_ConfigDirOverride(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	override := filepath.Join(t.TempDir(), "cfg")
	t.Setenv(ccsubagentsConfigDirEnv, override)

	globalPath, localPath := resolveCCSubagentsSettingsPaths(home, cwd)
	if globalPath != filepath.Join(override, "settings.json") {
		t.Fatalf("global path mismatch: got=%q want=%q", globalPath, filepath.Join(override, "settings.json"))
	}
	if localPath != filepath.Join(cwd, "ccsubagents", "settings.json") {
		t.Fatalf("local path mismatch: got=%q want=%q", localPath, filepath.Join(cwd, "ccsubagents", "settings.json"))
	}
}

func TestResolveMergedCCSubagentsSettings_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
	}{
		{name: "autostart type", content: `{"autostart-webui": "yes"}`, key: "autostart-webui"},
		{name: "no-auth type", content: `{"no-auth": "true"}`, key: "no-auth"},
		{name: "webui-port type", content: `{"webui-port": "19130"}`, key: "webui-port"},
		{name: "webui-port range", content: `{"webui-port": 70000}`, key: "webui-port"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			globalPath, _ := resolveCCSubagentsSettingsPaths(home, cwd)
			writeSettingsFile(t, globalPath, tc.content)

			_, err := resolveMergedCCSubagentsSettings(home, cwd)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected %s context in error, got %v", tc.key, err)
			}
		})
	}
}
