package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestSettingsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}
}

func TestLoadMergedInstallSettings_NoAuthAndWebUIPortMerge(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, localPath := ResolveSettingsPaths(home, cwd)

	writeTestSettingsFile(t, globalPath, `{"no-auth": true, "webui-port": 19130, "autostart-webui": true}`)
	writeTestSettingsFile(t, localPath, `{"no-auth": false, "webui-port": 19133}`)

	settings, err := LoadMergedInstallSettings(home, cwd)
	if err != nil {
		t.Fatalf("LoadMergedInstallSettings returned error: %v", err)
	}
	if settings.NoAuth {
		t.Fatalf("expected local no-auth override to false")
	}
	if settings.WebUIPort != 19133 {
		t.Fatalf("webui-port mismatch: got=%d want=%d", settings.WebUIPort, 19133)
	}
	if !settings.AutostartWebUI {
		t.Fatalf("expected autostart-webui to remain true from global settings")
	}
}

func TestLoadMergedInstallSettings_NoAuthTypeMismatch(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := ResolveSettingsPaths(home, cwd)
	writeTestSettingsFile(t, globalPath, `{"no-auth": "true"}`)

	_, err := LoadMergedInstallSettings(home, cwd)
	if err == nil {
		t.Fatalf("expected no-auth type mismatch error")
	}
	if !strings.Contains(err.Error(), "no-auth") {
		t.Fatalf("expected no-auth error context, got %v", err)
	}
}

func TestLoadMergedInstallSettings_WebUIPortValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "type mismatch", content: `{"webui-port": "19130"}`},
		{name: "range underflow", content: `{"webui-port": 0}`},
		{name: "range overflow", content: `{"webui-port": 70000}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			globalPath, _ := ResolveSettingsPaths(home, cwd)
			writeTestSettingsFile(t, globalPath, tc.content)

			_, err := LoadMergedInstallSettings(home, cwd)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), "webui-port") {
				t.Fatalf("expected webui-port error context, got %v", err)
			}
		})
	}
}
