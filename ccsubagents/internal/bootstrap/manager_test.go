package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func mustWriteZipFile(t *testing.T, path string, files map[string]string) {
	t.Helper()
	b := zipBytes(t, files)
	if err := os.WriteFile(path, b, stateFilePerm); err != nil {
		t.Fatalf("write zip file: %v", err)
	}
}

func TestApplySettingsEdit_AppendsWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"editor.fontSize": 14,
		"chat.agentFilesLocations": map[string]any{
			"/existing": true,
		},
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	edit, err := applySettingsEdit(settingsPath, "/managed/agents", nil)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}
	if !edit.Added {
		t.Fatalf("expected Added=true")
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	if root["editor.fontSize"].(float64) != 14 {
		t.Fatalf("expected editor.fontSize preserved")
	}

	locations := root["chat.agentFilesLocations"].(map[string]any)
	if len(locations) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(locations))
	}
	if locations["/existing"] != true || locations["/managed/agents"] != true {
		t.Fatalf("unexpected agentFilesLocations: %#v", locations)
	}
}

func TestApplySettingsEdit_WrongTypeFails(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"chat.agentFilesLocations": "invalid",
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	if _, err := applySettingsEdit(settingsPath, "/managed/agents", nil); err == nil {
		t.Fatalf("expected error for wrong chat.agentFilesLocations type")
	}
}

func TestApplySettingsEdit_MigratesTrackedPreviousPath(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"chat.agentFilesLocations": map[string]any{
			"/home/user/.local/share/ccsubagents/agents": true,
			"/existing": true,
		},
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{
				AgentPath: "/home/user/.local/share/ccsubagents/agents",
				Added:     true,
			},
		},
	}

	edit, err := applySettingsEdit(settingsPath, "~/.local/share/ccsubagents/agents", previous)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}
	if !edit.Added {
		t.Fatalf("expected Added=true")
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	locations := root["chat.agentFilesLocations"].(map[string]any)
	if locations["~/.local/share/ccsubagents/agents"] != true || locations["/existing"] != true {
		t.Fatalf("unexpected locations: %#v", locations)
	}
	if _, exists := locations["/home/user/.local/share/ccsubagents/agents"]; exists {
		t.Fatalf("expected tracked previous absolute path removed: %#v", locations)
	}
}

func TestApplySettingsEdit_CrossTargetFallbackIgnoresConcretePathMismatch(t *testing.T) {
	dir := t.TempDir()
	sourceSettingsPath := filepath.Join(dir, "source-settings.json")
	targetSettingsPath := filepath.Join(dir, "target-settings.json")

	seed := map[string]any{
		"chat.agentFilesLocations": map[string]any{
			"/legacy/path":    true,
			"/managed/agents": true,
			"/existing":       true,
		},
	}
	if err := writeJSONFile(targetSettingsPath, seed); err != nil {
		t.Fatalf("seed target settings file: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{
				File:      sourceSettingsPath,
				AgentPath: "/legacy/path",
				Added:     true,
			},
		},
	}

	edit, err := applySettingsEdit(targetSettingsPath, "/managed/agents", previous)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}
	if edit.Added {
		t.Fatalf("expected Added=false when target already contained managed path")
	}

	root, err := readJSONFile(targetSettingsPath)
	if err != nil {
		t.Fatalf("read target settings file: %v", err)
	}
	locations := root["chat.agentFilesLocations"].(map[string]any)
	if locations["/legacy/path"] != true {
		t.Fatalf("expected legacy path preserved for mismatched tracked file: %#v", locations)
	}
	if locations["/managed/agents"] != true {
		t.Fatalf("expected managed path preserved: %#v", locations)
	}
}

func TestApplySettingsEdit_LegacyFallbackUsesSingleNoFileEntry(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	seed := map[string]any{
		"chat.agentFilesLocations": map[string]any{
			"/legacy/path": true,
			"/existing":    true,
		},
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{
				File:      "",
				AgentPath: "/legacy/path",
				Added:     true,
			},
		},
	}

	edit, err := applySettingsEdit(settingsPath, "/managed/agents", previous)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}
	if !edit.Added {
		t.Fatalf("expected Added=true")
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	locations := root["chat.agentFilesLocations"].(map[string]any)
	if locations["/managed/agents"] != true || locations["/existing"] != true {
		t.Fatalf("unexpected locations after legacy fallback migration: %#v", locations)
	}
	if _, exists := locations["/legacy/path"]; exists {
		t.Fatalf("expected legacy tracked path removed: %#v", locations)
	}
}

func TestApplyMCPEdit_PreservesExistingServersAndInputs(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	seed := map[string]any{
		"servers": map[string]any{
			"foo": map[string]any{"command": "foo"},
		},
		"inputs": map[string]any{
			"token": map[string]any{"type": "promptString"},
		},
	}
	if err := writeJSONFile(mcpPath, seed); err != nil {
		t.Fatalf("seed mcp file: %v", err)
	}

	edit, err := applyMCPEdit(mcpPath, "/usr/local/bin/local-artifact-mcp", nil)
	if err != nil {
		t.Fatalf("apply mcp edit: %v", err)
	}
	if edit.HadPrevious {
		t.Fatalf("expected HadPrevious=false")
	}

	root, err := readJSONFile(mcpPath)
	if err != nil {
		t.Fatalf("read mcp file: %v", err)
	}

	servers := root["servers"].(map[string]any)
	if _, ok := servers["foo"]; !ok {
		t.Fatalf("expected existing server preserved")
	}
	artifactServer, ok := servers[mcpServerKey].(map[string]any)
	if !ok {
		t.Fatalf("expected artifact-mcp server object")
	}
	if artifactServer["command"].(string) != "/usr/local/bin/local-artifact-mcp" {
		t.Fatalf("unexpected artifact-mcp command: %#v", artifactServer)
	}

	if _, ok := root["inputs"].(map[string]any)["token"]; !ok {
		t.Fatalf("expected inputs preserved")
	}
}

func TestApplyMCPEdit_PreservesPriorBaselineForUninstall(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	seed := map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{"command": "/usr/local/bin/local-artifact-mcp"},
		},
	}
	if err := writeJSONFile(mcpPath, seed); err != nil {
		t.Fatalf("seed mcp file: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			MCP: mcpEdit{Touched: true, HadPrevious: false},
		},
	}

	edit, err := applyMCPEdit(mcpPath, "/usr/local/bin/local-artifact-mcp", previous)
	if err != nil {
		t.Fatalf("apply mcp edit: %v", err)
	}
	if edit.HadPrevious {
		t.Fatalf("expected HadPrevious=false from prior baseline")
	}
}

func TestApplyMCPEdit_CrossTargetFallbackUsesCurrentFileBaseline(t *testing.T) {
	dir := t.TempDir()
	sourceMCPPath := filepath.Join(dir, "source-mcp.json")
	targetMCPPath := filepath.Join(dir, "target-mcp.json")

	seed := map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{"command": "/usr/bin/target-existing"},
			"foo":        map[string]any{"command": "foo"},
		},
	}
	if err := writeJSONFile(targetMCPPath, seed); err != nil {
		t.Fatalf("seed target mcp file: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			MCP: mcpEdit{File: sourceMCPPath, Touched: true, HadPrevious: false},
		},
	}

	edit, err := applyMCPEdit(targetMCPPath, "/usr/local/bin/local-artifact-mcp", previous)
	if err != nil {
		t.Fatalf("apply mcp edit: %v", err)
	}
	if !edit.HadPrevious {
		t.Fatalf("expected HadPrevious=true from target file baseline")
	}

	var previousServer map[string]any
	if err := json.Unmarshal(edit.Previous, &previousServer); err != nil {
		t.Fatalf("decode previous server: %v", err)
	}
	if previousServer["command"] != "/usr/bin/target-existing" {
		t.Fatalf("expected target baseline preserved, got %#v", previousServer)
	}
}

func TestApplyMCPEdit_LegacyFallbackUsesSingleNoFileEntry(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")

	seed := map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{"command": "/usr/bin/current-existing"},
		},
	}
	if err := writeJSONFile(mcpPath, seed); err != nil {
		t.Fatalf("seed mcp file: %v", err)
	}

	legacyPrevious, err := json.Marshal(map[string]any{"command": "/usr/bin/legacy-previous"})
	if err != nil {
		t.Fatalf("marshal legacy previous: %v", err)
	}

	previous := &trackedState{
		JSONEdits: trackedJSONOps{
			MCP: mcpEdit{
				File:        "",
				Key:         mcpServerKey,
				Touched:     true,
				HadPrevious: true,
				Previous:    legacyPrevious,
			},
		},
	}

	edit, err := applyMCPEdit(mcpPath, "/usr/local/bin/local-artifact-mcp", previous)
	if err != nil {
		t.Fatalf("apply mcp edit: %v", err)
	}
	if !edit.HadPrevious {
		t.Fatalf("expected HadPrevious=true from legacy tracked baseline")
	}

	var previousServer map[string]any
	if err := json.Unmarshal(edit.Previous, &previousServer); err != nil {
		t.Fatalf("decode previous server: %v", err)
	}
	if previousServer["command"] != "/usr/bin/legacy-previous" {
		t.Fatalf("expected legacy tracked baseline to be reused, got %#v", previousServer)
	}
}

func TestRevertSettingsEdit_RemovesOnlyTrackedValue(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"chat.agentFilesLocations": map[string]any{
			"/existing":       true,
			"/managed/agents": true,
			"/other":          true,
		},
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	err := revertSettingsEdit(settingsEdit{File: settingsPath, AgentPath: "/managed/agents", Added: true})
	if err != nil {
		t.Fatalf("revert settings edit: %v", err)
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	locations := root["chat.agentFilesLocations"].(map[string]any)
	if len(locations) != 2 || locations["/existing"] != true || locations["/other"] != true {
		t.Fatalf("unexpected map after revert: %#v", locations)
	}
	if _, ok := locations["/managed/agents"]; ok {
		t.Fatalf("expected managed path removed: %#v", locations)
	}
}

func TestApplySettingsEdit_UsesTopLevelObjectFormat(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"chat": map[string]any{
			"agentFilesLocations": []any{"/existing"},
		},
	}
	if err := writeJSONFile(settingsPath, seed); err != nil {
		t.Fatalf("seed settings file: %v", err)
	}

	_, err := applySettingsEdit(settingsPath, "/managed/agents", nil)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	locations := root["chat.agentFilesLocations"].(map[string]any)
	if len(locations) != 1 || locations["/managed/agents"] != true {
		t.Fatalf("unexpected top-level object: %#v", locations)
	}
}

func TestRevertMCPEdit_RestoresPreviousOrRemovesTrackedServer(t *testing.T) {
	t.Run("restore previous", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		if err := writeJSONFile(path, map[string]any{"servers": map[string]any{mcpServerKey: map[string]any{"command": "new"}}}); err != nil {
			t.Fatalf("seed mcp file: %v", err)
		}

		prev, _ := json.Marshal(map[string]any{"command": "old"})
		err := revertMCPEdit(mcpEdit{File: path, Key: mcpServerKey, Touched: true, HadPrevious: true, Previous: prev})
		if err != nil {
			t.Fatalf("revert mcp edit: %v", err)
		}

		root, err := readJSONFile(path)
		if err != nil {
			t.Fatalf("read mcp file: %v", err)
		}
		servers := root["servers"].(map[string]any)
		if servers[mcpServerKey].(map[string]any)["command"].(string) != "old" {
			t.Fatalf("expected restored previous value")
		}
	})

	t.Run("remove inserted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		if err := writeJSONFile(path, map[string]any{"servers": map[string]any{mcpServerKey: map[string]any{"command": "new"}}}); err != nil {
			t.Fatalf("seed mcp file: %v", err)
		}

		err := revertMCPEdit(mcpEdit{File: path, Key: mcpServerKey, Touched: true, HadPrevious: false})
		if err != nil {
			t.Fatalf("revert mcp edit: %v", err)
		}

		root, err := readJSONFile(path)
		if err != nil {
			t.Fatalf("read mcp file: %v", err)
		}
		servers := root["servers"].(map[string]any)
		if _, ok := servers[mcpServerKey]; ok {
			t.Fatalf("expected inserted server to be removed")
		}
	})
}

func TestIsAllowedManagedPath(t *testing.T) {
	agentsDir := filepath.Join(string(os.PathSeparator), "home", "user", ".local", "share", "ccsubagents", "agents")
	binaryDir := filepath.Join(string(os.PathSeparator), "home", "user", binaryInstallDirDefaultRel)
	allowedBinaries := []string{
		filepath.Join(binaryDir, assetArtifactMCP),
		filepath.Join(binaryDir, assetArtifactWeb),
	}

	if !isAllowedManagedPath(filepath.Join(agentsDir, "one.agent.md"), agentsDir, allowedBinaries) {
		t.Fatalf("expected managed agents path to be allowed")
	}
	if !isAllowedManagedPath(filepath.Join(binaryDir, assetArtifactMCP), agentsDir, allowedBinaries) {
		t.Fatalf("expected managed mcp binary path to be allowed")
	}
	if isAllowedManagedPath(filepath.Join(string(os.PathSeparator), "tmp", "oops"), agentsDir, allowedBinaries) {
		t.Fatalf("expected unrelated path to be denied")
	}
}

func TestResolveInstallPaths_Defaults(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	t.Setenv(binaryInstallDirEnv, "")
	t.Setenv(settingsPathEnv, "")
	t.Setenv(mcpConfigPathEnv, "")

	paths := resolveInstallPaths(home)
	if paths.binaryDir != filepath.Join(home, binaryInstallDirDefaultRel) {
		t.Fatalf("expected default binary dir under home, got %q", paths.binaryDir)
	}
	if paths.stable.settingsPath != filepath.Join(home, settingsStableRelativePath) {
		t.Fatalf("expected default stable settings path under home, got %q", paths.stable.settingsPath)
	}
	if paths.stable.mcpPath != filepath.Join(home, mcpConfigStableRelativePath) {
		t.Fatalf("expected default stable mcp path under home, got %q", paths.stable.mcpPath)
	}
	if paths.insiders.settingsPath != filepath.Join(home, settingsInsidersRelativePath) {
		t.Fatalf("expected default insiders settings path under home, got %q", paths.insiders.settingsPath)
	}
	if paths.insiders.mcpPath != filepath.Join(home, mcpConfigInsidersRelativePath) {
		t.Fatalf("expected default insiders mcp path under home, got %q", paths.insiders.mcpPath)
	}
}

func TestResolveInstallPaths_EnvOverrides(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	t.Setenv(binaryInstallDirEnv, "~/bin")
	t.Setenv(settingsPathEnv, "custom/settings.json")
	t.Setenv(mcpConfigPathEnv, filepath.Join(string(os.PathSeparator), "tmp", "custom-mcp.json"))

	paths := resolveInstallPaths(home)
	if paths.binaryDir != filepath.Join(home, "bin") {
		t.Fatalf("expected home-relative binary dir, got %q", paths.binaryDir)
	}
	if paths.stable.settingsPath != filepath.Join(home, "custom", "settings.json") {
		t.Fatalf("expected home-relative stable settings path, got %q", paths.stable.settingsPath)
	}
	if paths.stable.mcpPath != filepath.Join(string(os.PathSeparator), "tmp", "custom-mcp.json") {
		t.Fatalf("expected absolute stable mcp path override, got %q", paths.stable.mcpPath)
	}
	if paths.insiders.settingsPath != filepath.Join(home, "custom", "settings.json") {
		t.Fatalf("expected home-relative insiders settings path, got %q", paths.insiders.settingsPath)
	}
	if paths.insiders.mcpPath != filepath.Join(string(os.PathSeparator), "tmp", "custom-mcp.json") {
		t.Fatalf("expected absolute insiders mcp path override, got %q", paths.insiders.mcpPath)
	}
}

func TestResolveInstallTargets(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	t.Setenv(settingsPathEnv, "")
	t.Setenv(mcpConfigPathEnv, "")
	paths := resolveInstallPaths(home)

	t.Run("stable", func(t *testing.T) {
		targets, err := resolveInstallTargets(paths, installDestinationStable)
		if err != nil {
			t.Fatalf("resolve stable targets: %v", err)
		}
		if len(targets) != 1 {
			t.Fatalf("expected 1 stable target, got %d", len(targets))
		}
		if targets[0].settingsPath != paths.stable.settingsPath || targets[0].mcpPath != paths.stable.mcpPath {
			t.Fatalf("unexpected stable target: %#v", targets[0])
		}
	})

	t.Run("insiders", func(t *testing.T) {
		targets, err := resolveInstallTargets(paths, installDestinationInsiders)
		if err != nil {
			t.Fatalf("resolve insiders targets: %v", err)
		}
		if len(targets) != 1 {
			t.Fatalf("expected 1 insiders target, got %d", len(targets))
		}
		if targets[0].settingsPath != paths.insiders.settingsPath || targets[0].mcpPath != paths.insiders.mcpPath {
			t.Fatalf("unexpected insiders target: %#v", targets[0])
		}
	})

	t.Run("both", func(t *testing.T) {
		targets, err := resolveInstallTargets(paths, installDestinationBoth)
		if err != nil {
			t.Fatalf("resolve both targets: %v", err)
		}
		if len(targets) != 2 {
			t.Fatalf("expected 2 targets for both, got %d", len(targets))
		}

		got := map[string]bool{}
		for _, target := range targets {
			got[target.settingsPath+"\n"+target.mcpPath] = true
		}

		if !got[paths.stable.settingsPath+"\n"+paths.stable.mcpPath] {
			t.Fatalf("expected stable target in both set: %#v", targets)
		}
		if !got[paths.insiders.settingsPath+"\n"+paths.insiders.mcpPath] {
			t.Fatalf("expected insiders target in both set: %#v", targets)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := resolveInstallTargets(paths, installDestination("nope")); err == nil {
			t.Fatalf("expected error for invalid destination")
		}
	})
}

func TestResolveUpdateTargets_UsesTrackedMultiEdits(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	paths := resolveInstallPaths(home)

	previous := &trackedState{
		JSONEdits: trackedJSONOpsFromEdits(
			[]settingsEdit{
				{File: filepath.Join(home, ".vscode-server", "data", "Machine", "settings.json")},
				{File: filepath.Join(home, ".vscode-server-insiders", "data", "Machine", "settings.json")},
			},
			[]mcpEdit{
				{File: filepath.Join(home, ".vscode-server", "data", "User", "mcp.json"), Touched: true},
				{File: filepath.Join(home, ".vscode-server-insiders", "data", "User", "mcp.json"), Touched: true},
			},
		),
	}

	targets := resolveUpdateTargets(paths, previous)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets from tracked multi-edits, got %d", len(targets))
	}

	got := map[string]bool{}
	for _, target := range targets {
		got[target.settingsPath+"\n"+target.mcpPath] = true
	}

	stableKey := filepath.Join(home, ".vscode-server", "data", "Machine", "settings.json") + "\n" + filepath.Join(home, ".vscode-server", "data", "User", "mcp.json")
	insidersKey := filepath.Join(home, ".vscode-server-insiders", "data", "Machine", "settings.json") + "\n" + filepath.Join(home, ".vscode-server-insiders", "data", "User", "mcp.json")
	if !got[stableKey] || !got[insidersKey] {
		t.Fatalf("unexpected update targets: %#v", targets)
	}
}

func TestRun_InstallPromptsForDestination(t *testing.T) {
	m := NewManager()
	m.SetInstallPromptIO(strings.NewReader(""), io.Discard)

	err := m.Run(context.Background(), CommandInstall)
	if err == nil {
		t.Fatalf("expected install prompt to fail on canceled selection")
	}
	if !strings.Contains(err.Error(), "selection canceled") {
		t.Fatalf("expected canceled-selection error, got %v", err)
	}
}

func TestRun_UpdateDoesNotPromptForDestination(t *testing.T) {
	m := NewManager()
	m.SetInstallPromptIO(errorReader{err: errors.New("prompt read should not occur")}, io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.Run(ctx, CommandUpdate)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled for update, got %v", err)
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestInstallOrUpdate_TracksBothDestinationTargets(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
	})

	m := &Manager{
		httpClient: successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive),
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil },
		installBinary: func(src, dst string) error {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, binaryFilePerm)
		},
		installDestination: installDestinationBoth,
	}

	if err := m.installOrUpdate(context.Background(), false); err != nil {
		t.Fatalf("install should succeed: %v", err)
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		t.Fatalf("load tracked state: %v", err)
	}
	if len(state.JSONEdits.allSettingsEdits()) != 2 {
		t.Fatalf("expected 2 tracked settings edits, got %d", len(state.JSONEdits.allSettingsEdits()))
	}
	if len(state.JSONEdits.allMCPEdits()) != 2 {
		t.Fatalf("expected 2 tracked mcp edits, got %d", len(state.JSONEdits.allMCPEdits()))
	}

	paths := resolveInstallPaths(home)
	for _, settingsPath := range []string{paths.stable.settingsPath, paths.insiders.settingsPath} {
		root, err := readJSONFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings %s: %v", settingsPath, err)
		}
		locations, ok := root[settingsAgentPathKey].(map[string]any)
		if !ok {
			t.Fatalf("expected agent locations object in %s", settingsPath)
		}
		agentPath := "~/.local/share/ccsubagents/agents"
		if locations[agentPath] != true {
			t.Fatalf("expected managed agent path in %s", settingsPath)
		}
	}

	for _, mcpPath := range []string{paths.stable.mcpPath, paths.insiders.mcpPath} {
		root, err := readJSONFile(mcpPath)
		if err != nil {
			t.Fatalf("read mcp config %s: %v", mcpPath, err)
		}
		servers, ok := root["servers"].(map[string]any)
		if !ok {
			t.Fatalf("expected servers object in %s", mcpPath)
		}
		artifact, ok := servers[mcpServerKey].(map[string]any)
		if !ok {
			t.Fatalf("expected managed mcp server in %s", mcpPath)
		}
		if artifact["command"] != "~/.local/bin/local-artifact-mcp" {
			t.Fatalf("unexpected command for %s: %#v", mcpPath, artifact)
		}
	}
}

func TestToHomeTildePath(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "user")

	got := toHomeTildePath(home, filepath.Join(home, ".local", "share", "ccsubagents", "agents"))
	if got != "~/.local/share/ccsubagents/agents" {
		t.Fatalf("expected home-relative tilde path, got %q", got)
	}

	got = toHomeTildePath(home, home)
	if got != "~" {
		t.Fatalf("expected home root to map to ~, got %q", got)
	}

	got = toHomeTildePath(home, filepath.Join(string(os.PathSeparator), "opt", "tool"))
	if got != "/opt/tool" {
		t.Fatalf("expected non-home path to remain absolute, got %q", got)
	}
}

func TestInstallOrUpdate_AttestationFailureBeforeMutation(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	artifactBundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp":          "mcp-binary",
		"nested/local-artifact-web":   "web-binary",
		"nested/not-needed-something": "ignored",
	})

	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		status := http.StatusOK
		switch req.URL.String() {
		case releaseLatestURL:
			body = fmt.Sprintf(`{"id":101,"tag_name":"v1.2.3","assets":[{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"}]}`,
				assetAgentsZip, assetAgentsZip,
				assetLocalArtifactZip, assetLocalArtifactZip,
			)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetAgentsZip:
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(agentsArchive)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetLocalArtifactZip:
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(artifactBundleArchive)), Header: make(http.Header)}, nil
		default:
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		}
	})}

	m := &Manager{
		httpClient: httpClient,
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("verification failed")
		},
	}

	err := m.installOrUpdate(context.Background(), false)
	if err == nil {
		t.Fatalf("expected attestation verification error")
	}
	if !strings.Contains(err.Error(), "attestation verification failed") {
		t.Fatalf("expected attestation error, got: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(home, ".local", "share", "ccsubagents", "agents")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected agents dir to remain absent, stat err: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "share", "ccsubagents", trackedFileName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected tracked state to remain absent, stat err: %v", statErr)
	}
}

func TestInstallOrUpdate_CorruptTrackedStateFails(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	trackedPath := filepath.Join(stateDir, trackedFileName)
	if err := os.WriteFile(trackedPath, []byte("{"), stateFilePerm); err != nil {
		t.Fatalf("seed corrupt tracked state: %v", err)
	}

	m := &Manager{
		httpClient: &http.Client{},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}

	err := m.installOrUpdate(context.Background(), false)
	if err == nil {
		t.Fatalf("expected corrupt tracked-state error")
	}
	if !strings.Contains(err.Error(), "tracked state is unreadable") {
		t.Fatalf("expected tracked-state message, got: %v", err)
	}
}

func TestInstallOrUpdate_UpdateStaleCleanupFailureRollsBackAndKeepsTrackedState(t *testing.T) {
	home := t.TempDir()
	agentsDir := filepath.Join(home, ".local", "share", "ccsubagents", "agents")
	staleDir := filepath.Join(agentsDir, "stale-dir")
	if err := os.MkdirAll(staleDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}

	staleOne := filepath.Join(agentsDir, "stale-one.agent.md")
	if err := os.WriteFile(staleOne, []byte("old-one"), stateFilePerm); err != nil {
		t.Fatalf("seed stale file one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "nested.agent.md"), []byte("old-two"), stateFilePerm); err != nil {
		t.Fatalf("seed stale directory content: %v", err)
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	previous := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   1,
		ReleaseTag:  "v-old",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed: managedState{
			Files: []string{staleOne, staleDir},
		},
	}

	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status := http.StatusOK
			switch req.URL.String() {
			case releaseLatestURL:
				body := fmt.Sprintf(`{"id":202,"tag_name":"v-new","assets":[{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"}]}`,
					assetAgentsZip, assetAgentsZip,
					assetLocalArtifactZip, assetLocalArtifactZip,
				)
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetAgentsZip:
				archive := zipBytes(t, map[string]string{"agents/current.agent.md": "new"})
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetLocalArtifactZip:
				archive := zipBytes(t, map[string]string{
					"release/local-artifact-mcp": "mcp",
					"local-artifact-web":         "web",
				})
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
			default:
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
		})},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil },
	}

	if err := m.saveTrackedState(stateDir, previous); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}
	trackedPath := filepath.Join(stateDir, trackedFileName)
	originalTracked, err := os.ReadFile(trackedPath)
	if err != nil {
		t.Fatalf("read seeded tracked state: %v", err)
	}

	m.installBinary = func(string, string) error { return nil }

	err = m.installOrUpdate(context.Background(), true)
	if err == nil {
		t.Fatalf("expected stale cleanup failure")
	}
	if !strings.Contains(err.Error(), "cannot snapshot directory for rollback") {
		t.Fatalf("expected stale cleanup error, got: %v", err)
	}

	staleOneData, err := os.ReadFile(staleOne)
	if err != nil {
		t.Fatalf("expected stale file one restored: %v", err)
	}
	if string(staleOneData) != "old-one" {
		t.Fatalf("expected stale file one content restored, got %q", string(staleOneData))
	}

	if _, statErr := os.Stat(filepath.Join(agentsDir, "current.agent.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected newly extracted file rolled back, stat err: %v", statErr)
	}

	currentTracked, err := os.ReadFile(trackedPath)
	if err != nil {
		t.Fatalf("read tracked state after failed update: %v", err)
	}
	if !bytes.Equal(originalTracked, currentTracked) {
		t.Fatalf("expected tracked state unchanged after failed update")
	}
}

func TestExtractAgentsArchive_StripsTopLevelAgentsDirectory(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "agents.zip")
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, stateDirPerm); err != nil {
		t.Fatalf("create dest: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"agents/root.agent.md":         "root",
		"agents/nested/child.agent.md": "child",
	})

	files, _, err := extractAgentsArchiveWithHook(zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	rootFile := filepath.Join(dest, "root.agent.md")
	nestedFile := filepath.Join(dest, "nested", "child.agent.md")
	if _, err := os.Stat(rootFile); err != nil {
		t.Fatalf("expected root file under destination root: %v", err)
	}
	if _, err := os.Stat(nestedFile); err != nil {
		t.Fatalf("expected nested file under destination root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no nested agents directory, stat err: %v", err)
	}

	fileSet := map[string]struct{}{}
	for _, file := range files {
		fileSet[file] = struct{}{}
	}
	if _, ok := fileSet[rootFile]; !ok {
		t.Fatalf("expected tracked extracted root file")
	}
	if _, ok := fileSet[nestedFile]; !ok {
		t.Fatalf("expected tracked extracted nested file")
	}
}

func TestUninstall_FailsWhenMCPServersObjectIsMalformed(t *testing.T) {
	home := t.TempDir()
	m := &Manager{
		httpClient: &http.Client{},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	mcpPath := filepath.Join(home, mcpConfigRelativePath)
	if err := os.MkdirAll(filepath.Dir(mcpPath), stateDirPerm); err != nil {
		t.Fatalf("create mcp dir: %v", err)
	}
	if err := os.WriteFile(mcpPath, []byte("{\"servers\":\"oops\"}\n"), stateFilePerm); err != nil {
		t.Fatalf("seed malformed mcp.json: %v", err)
	}

	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   1,
		ReleaseTag:  "v1.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed:     managedState{},
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{},
			MCP: mcpEdit{
				File:    mcpPath,
				Key:     mcpServerKey,
				Touched: true,
			},
		},
	}
	if err := m.saveTrackedState(stateDir, state); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	err := m.uninstall(context.Background())
	if err == nil {
		t.Fatalf("expected uninstall to fail on malformed mcp servers object")
	}
	if !strings.Contains(err.Error(), "mcp key servers") {
		t.Fatalf("expected explicit mcp servers error, got: %v", err)
	}
}

func TestUninstall_BothTargetMigrationRevertsPerTargetMetadata(t *testing.T) {
	home := t.TempDir()
	m := &Manager{
		httpClient: &http.Client{},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}

	paths := resolveInstallPaths(home)
	for _, configPath := range []string{paths.stable.settingsPath, paths.insiders.settingsPath, paths.stable.mcpPath, paths.insiders.mcpPath} {
		if err := os.MkdirAll(filepath.Dir(configPath), stateDirPerm); err != nil {
			t.Fatalf("create config parent directory: %v", err)
		}
	}

	agentPath := "~/.local/share/ccsubagents/agents"
	if err := writeJSONFile(paths.stable.settingsPath, map[string]any{
		settingsAgentPathKey: map[string]any{
			agentPath:      true,
			"/stable-keep": true,
		},
	}); err != nil {
		t.Fatalf("seed stable settings: %v", err)
	}
	if err := writeJSONFile(paths.insiders.settingsPath, map[string]any{
		settingsAgentPathKey: map[string]any{
			agentPath:        true,
			"/insiders-keep": true,
		},
	}); err != nil {
		t.Fatalf("seed insiders settings: %v", err)
	}

	if err := writeJSONFile(paths.stable.mcpPath, map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{"command": "~/.local/bin/local-artifact-mcp"},
		},
	}); err != nil {
		t.Fatalf("seed stable mcp: %v", err)
	}
	if err := writeJSONFile(paths.insiders.mcpPath, map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{"command": "~/.local/bin/local-artifact-mcp"},
			"foo":        map[string]any{"command": "foo"},
		},
	}); err != nil {
		t.Fatalf("seed insiders mcp: %v", err)
	}

	insidersPrevious, err := json.Marshal(map[string]any{"command": "/usr/bin/insiders-prev"})
	if err != nil {
		t.Fatalf("marshal insiders previous mcp: %v", err)
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   1,
		ReleaseTag:  "v1.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed:     managedState{},
		JSONEdits: trackedJSONOpsFromEdits(
			[]settingsEdit{
				{File: paths.stable.settingsPath, AgentPath: agentPath, Added: true},
				{File: paths.insiders.settingsPath, AgentPath: agentPath, Added: false},
			},
			[]mcpEdit{
				{File: paths.stable.mcpPath, Key: mcpServerKey, Touched: true, HadPrevious: false},
				{File: paths.insiders.mcpPath, Key: mcpServerKey, Touched: true, HadPrevious: true, Previous: insidersPrevious},
			},
		),
	}
	if err := m.saveTrackedState(stateDir, state); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should succeed: %v", err)
	}

	stableSettingsRoot, err := readJSONFile(paths.stable.settingsPath)
	if err != nil {
		t.Fatalf("read stable settings: %v", err)
	}
	stableLocations := stableSettingsRoot[settingsAgentPathKey].(map[string]any)
	if stableLocations["/stable-keep"] != true {
		t.Fatalf("expected stable keep path to remain: %#v", stableLocations)
	}
	if _, exists := stableLocations[agentPath]; exists {
		t.Fatalf("expected managed path removed from stable settings: %#v", stableLocations)
	}

	insidersSettingsRoot, err := readJSONFile(paths.insiders.settingsPath)
	if err != nil {
		t.Fatalf("read insiders settings: %v", err)
	}
	insidersLocations := insidersSettingsRoot[settingsAgentPathKey].(map[string]any)
	if insidersLocations["/insiders-keep"] != true || insidersLocations[agentPath] != true {
		t.Fatalf("expected insiders locations preserved: %#v", insidersLocations)
	}

	stableMCPRoot, err := readJSONFile(paths.stable.mcpPath)
	if err != nil {
		t.Fatalf("read stable mcp: %v", err)
	}
	stableServers := stableMCPRoot["servers"].(map[string]any)
	if _, exists := stableServers[mcpServerKey]; exists {
		t.Fatalf("expected managed server removed from stable mcp: %#v", stableServers)
	}

	insidersMCPRoot, err := readJSONFile(paths.insiders.mcpPath)
	if err != nil {
		t.Fatalf("read insiders mcp: %v", err)
	}
	insidersServers := insidersMCPRoot["servers"].(map[string]any)
	restored, ok := insidersServers[mcpServerKey].(map[string]any)
	if !ok {
		t.Fatalf("expected insiders managed server restored: %#v", insidersServers)
	}
	if restored["command"] != "/usr/bin/insiders-prev" {
		t.Fatalf("expected insiders previous command restored, got %#v", restored)
	}

	if _, err := os.Stat(filepath.Join(stateDir, trackedFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected tracked state removed, stat err: %v", err)
	}
}

func TestUninstall_AllowsTrackedConfigParentDirs(t *testing.T) {
	home := t.TempDir()
	m := &Manager{
		httpClient: &http.Client{},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	settingsParent := filepath.Dir(filepath.Join(home, settingsRelativePath))
	mcpParent := filepath.Dir(filepath.Join(home, mcpConfigRelativePath))
	if err := os.MkdirAll(settingsParent, stateDirPerm); err != nil {
		t.Fatalf("create settings parent dir: %v", err)
	}
	if err := os.MkdirAll(mcpParent, stateDirPerm); err != nil {
		t.Fatalf("create mcp parent dir: %v", err)
	}

	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   1,
		ReleaseTag:  "v1.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed: managedState{
			Dirs: []string{settingsParent, mcpParent},
		},
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{},
			MCP:      mcpEdit{},
		},
	}
	if err := m.saveTrackedState(stateDir, state); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should allow tracked config parent dirs: %v", err)
	}

	if _, err := os.Stat(settingsParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected settings parent dir removed, stat err: %v", err)
	}
	if _, err := os.Stat(mcpParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected mcp parent dir removed, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, trackedFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected tracked state removed, stat err: %v", err)
	}
}

func TestUninstall_IgnoresTypedNotEmptyDirectoryError(t *testing.T) {
	home := t.TempDir()
	m := &Manager{
		httpClient: &http.Client{},
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	agentsDir := filepath.Join(home, agentsRelativePath)
	if err := os.MkdirAll(agentsDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	keepFile := filepath.Join(agentsDir, "keep.agent.md")
	if err := os.WriteFile(keepFile, []byte("keep"), stateFilePerm); err != nil {
		t.Fatalf("seed non-empty agents dir: %v", err)
	}

	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   1,
		ReleaseTag:  "v1.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed: managedState{
			Dirs: []string{agentsDir},
		},
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit{},
			MCP:      mcpEdit{},
		},
	}
	if err := m.saveTrackedState(stateDir, state); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should ignore typed ENOTEMPTY directory removal errors: %v", err)
	}

	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("expected non-empty agents directory contents to remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, trackedFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected tracked state removed, stat err: %v", err)
	}
}
