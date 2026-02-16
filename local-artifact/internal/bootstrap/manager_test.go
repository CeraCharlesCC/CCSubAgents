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
		"editor.fontSize":          14,
		"chat.agentFilesLocations": []any{"/existing"},
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

	arr := root["chat.agentFilesLocations"].([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(arr))
	}
	if arr[0].(string) != "/existing" || arr[1].(string) != "/managed/agents" {
		t.Fatalf("unexpected agentFilesLocations: %#v", arr)
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

func TestRevertSettingsEdit_RemovesOnlyTrackedValue(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	seed := map[string]any{
		"chat.agentFilesLocations": []any{"/existing", "/managed/agents", "/other"},
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
	arr := root["chat.agentFilesLocations"].([]any)
	if len(arr) != 2 || arr[0].(string) != "/existing" || arr[1].(string) != "/other" {
		t.Fatalf("unexpected array after revert: %#v", arr)
	}
}

func TestApplySettingsEdit_NestedFallbackRemainsSupported(t *testing.T) {
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

	edit, err := applySettingsEdit(settingsPath, "/managed/agents", nil)
	if err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}
	if edit.Mode != settingsModeNested {
		t.Fatalf("expected nested mode, got %q", edit.Mode)
	}

	root, err := readJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	arr := root["chat"].(map[string]any)["agentFilesLocations"].([]any)
	if len(arr) != 2 || arr[1].(string) != "/managed/agents" {
		t.Fatalf("unexpected nested array: %#v", arr)
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
	agentsDir := filepath.Join(string(os.PathSeparator), "home", "user", ".copilot", "agents")
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
	if paths.settingsPath != filepath.Join(home, settingsRelativePath) {
		t.Fatalf("expected default settings path under home, got %q", paths.settingsPath)
	}
	if paths.mcpPath != filepath.Join(home, mcpConfigRelativePath) {
		t.Fatalf("expected default mcp path under home, got %q", paths.mcpPath)
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
	if paths.settingsPath != filepath.Join(home, "custom", "settings.json") {
		t.Fatalf("expected home-relative settings path, got %q", paths.settingsPath)
	}
	if paths.mcpPath != filepath.Join(string(os.PathSeparator), "tmp", "custom-mcp.json") {
		t.Fatalf("expected absolute mcp path override, got %q", paths.mcpPath)
	}
}

func TestInstallOrUpdate_AttestationFailureBeforeMutation(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})

	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		status := http.StatusOK
		switch req.URL.String() {
		case releaseLatestURL:
			body = fmt.Sprintf(`{"id":101,"tag_name":"v1.2.3","assets":[{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"}]}`,
				assetAgentsZip, assetAgentsZip,
				assetArtifactMCP, assetArtifactMCP,
				assetArtifactWeb, assetArtifactWeb,
			)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetAgentsZip:
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(agentsArchive)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetArtifactMCP:
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("mcp-binary")), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetArtifactWeb:
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("web-binary")), Header: make(http.Header)}, nil
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

	if _, statErr := os.Stat(filepath.Join(home, ".copilot", "agents")); !errors.Is(statErr, os.ErrNotExist) {
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
	agentsDir := filepath.Join(home, ".copilot", "agents")
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
				body := fmt.Sprintf(`{"id":202,"tag_name":"v-new","assets":[{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"},{"name":"%s","browser_download_url":"https://example.invalid/assets/%s"}]}`,
					assetAgentsZip, assetAgentsZip,
					assetArtifactMCP, assetArtifactMCP,
					assetArtifactWeb, assetArtifactWeb,
				)
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetAgentsZip:
				archive := zipBytes(t, map[string]string{"agents/current.agent.md": "new"})
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetArtifactMCP:
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("mcp")), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetArtifactWeb:
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("web")), Header: make(http.Header)}, nil
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

	prevInstallBinary := installBinaryFunc
	installBinaryFunc = func(string, string) error { return nil }
	t.Cleanup(func() { installBinaryFunc = prevInstallBinary })

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
