package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
)

func writeSettingsFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), stateDirPerm); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), stateFilePerm); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
}

func TestLoadMergedInstallSettings_LocalOverridesGlobalAutostart(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, localPath := config.ResolveSettingsPaths(home, cwd)

	writeSettingsFixture(t, globalPath, `{"autostart-webui": true, "pinned-version": "v1.2.3"}`)
	writeSettingsFixture(t, localPath, `{"autostart-webui": false}`)

	settings, err := config.LoadMergedInstallSettings(home, cwd)
	if err != nil {
		t.Fatalf("load merged settings: %v", err)
	}
	if settings.AutostartWebUI {
		t.Fatalf("expected local autostart override to be false")
	}
	if settings.PinnedVersion != "v1.2.3" {
		t.Fatalf("expected global pin to remain when local did not override, got %q", settings.PinnedVersion)
	}
}

func TestLoadMergedInstallSettings_LocalClearsGlobalPinnedVersion(t *testing.T) {
	t.Run("null clears pin", func(t *testing.T) {
		home := t.TempDir()
		cwd := t.TempDir()
		globalPath, localPath := config.ResolveSettingsPaths(home, cwd)

		writeSettingsFixture(t, globalPath, `{"pinned-version": "v1.2.3"}`)
		writeSettingsFixture(t, localPath, `{"pinned-version": null}`)

		settings, err := config.LoadMergedInstallSettings(home, cwd)
		if err != nil {
			t.Fatalf("load merged settings: %v", err)
		}
		if settings.PinnedVersion != "" {
			t.Fatalf("expected local null to clear global pin, got %q", settings.PinnedVersion)
		}
	})

	t.Run("string none clears pin", func(t *testing.T) {
		home := t.TempDir()
		cwd := t.TempDir()
		globalPath, localPath := config.ResolveSettingsPaths(home, cwd)

		writeSettingsFixture(t, globalPath, `{"pinned-version": "v1.2.3"}`)
		writeSettingsFixture(t, localPath, `{"pinned-version": "none"}`)

		settings, err := config.LoadMergedInstallSettings(home, cwd)
		if err != nil {
			t.Fatalf("load merged settings: %v", err)
		}
		if settings.PinnedVersion != "" {
			t.Fatalf("expected local "+`"none"`+" to clear global pin, got %q", settings.PinnedVersion)
		}
	})
}

func TestLoadMergedInstallSettings_TypeErrors(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := config.ResolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version": 123}`)

	_, err := config.LoadMergedInstallSettings(home, cwd)
	if err == nil {
		t.Fatalf("expected type error for non-string pinned-version")
	}
	if !strings.Contains(err.Error(), "pinned-version") {
		t.Fatalf("expected pinned-version type error, got %v", err)
	}
}

func TestLoadMergedInstallSettings_MissingFilesAreEmpty(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	settings, err := config.LoadMergedInstallSettings(home, cwd)
	if err != nil {
		t.Fatalf("expected missing settings files to be treated as empty, got %v", err)
	}
	if settings.AutostartWebUI {
		t.Fatalf("expected default autostart to be false")
	}
	if settings.PinnedVersion != "" {
		t.Fatalf("expected default pinned version to be empty, got %q", settings.PinnedVersion)
	}
}

func TestChoosePinWritePath_PrefersLocalWhenDirectoryExists(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "ccsubagents"), stateDirPerm); err != nil {
		t.Fatalf("create local ccsubagents directory: %v", err)
	}

	path, scope, err := config.ChoosePinWritePath(cwd, home)
	if err != nil {
		t.Fatalf("choose pin write path: %v", err)
	}
	_, localPath := config.ResolveSettingsPaths(home, cwd)
	if path != localPath {
		t.Fatalf("expected local write path %q, got %q", localPath, path)
	}
	if scope != config.SettingsScopeLocal {
		t.Fatalf("expected local scope, got %q", scope)
	}
}

func TestChoosePinWritePath_FallsBackToGlobal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	path, scope, err := config.ChoosePinWritePath(cwd, home)
	if err != nil {
		t.Fatalf("choose pin write path: %v", err)
	}
	globalPath, _ := config.ResolveSettingsPaths(home, cwd)
	if path != globalPath {
		t.Fatalf("expected global write path %q, got %q", globalPath, path)
	}
	if scope != config.SettingsScopeGlobal {
		t.Fatalf("expected global scope, got %q", scope)
	}
}

func TestWritePinnedVersion_PreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeSettingsFixture(t, settingsPath, `{"autostart-webui": true, "custom": {"nested": 1}}`)

	if err := config.WritePinnedVersion(settingsPath, "1.2.3", stateDirPerm, stateFilePerm); err != nil {
		t.Fatalf("write pinned version: %v", err)
	}

	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("decode settings file: %v", err)
	}
	if root["pinned-version"] != "v1.2.3" {
		t.Fatalf("expected pinned-version to be normalized and persisted, got %#v", root["pinned-version"])
	}
	if root["autostart-webui"] != true {
		t.Fatalf("expected autostart-webui to remain true")
	}
	custom, ok := root["custom"].(map[string]any)
	if !ok {
		t.Fatalf("expected custom object to be preserved")
	}
	if custom["nested"] != float64(1) {
		t.Fatalf("expected custom nested value preserved, got %#v", custom["nested"])
	}
}
