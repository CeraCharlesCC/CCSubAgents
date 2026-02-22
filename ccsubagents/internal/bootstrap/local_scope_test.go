package bootstrap

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallOrUpdateLocal_RollbackRestoresIgnoreFile_OnPostIgnoreFailure(t *testing.T) {
	installRoot := t.TempDir()
	excludePath := filepath.Join(installRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), stateDirPerm); err != nil {
		t.Fatalf("create exclude dir: %v", err)
	}
	originalExclude := "# existing exclude rules\n"
	if err := os.WriteFile(excludePath, []byte(originalExclude), stateFilePerm); err != nil {
		t.Fatalf("seed exclude file: %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(stateDir, trackedFileName), stateDirPerm); err != nil {
		t.Fatalf("create tracked-state collision dir: %v", err)
	}

	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
	})

	m := &Manager{
		httpClient: successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive),
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil },
		installBinary: func(src, dst string) error {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, binaryFilePerm)
		},
	}

	err := m.installOrUpdateLocal(context.Background(), localInstallConfig{
		isUpdate: false,
		location: localScopeLocation{
			installRoot: installRoot,
			repoRoot:    installRoot,
			inGitRepo:   true,
		},
		mode:     localInstallModePersonal,
		stateDir: stateDir,
		state:    &trackedState{Version: trackedSchemaVersion},
	})
	if err == nil {
		t.Fatalf("expected install failure after ignore edits")
	}
	if !strings.Contains(err.Error(), "replace tracked state") {
		t.Fatalf("expected tracked-state replace failure, got %v", err)
	}

	gotExclude, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude file after rollback: %v", err)
	}
	if string(gotExclude) != originalExclude {
		t.Fatalf("expected exclude file rollback to original content; got %q", string(gotExclude))
	}
}

func TestInstallLocal_TeamRerunPreservesFullManagedState(t *testing.T) {
	home := t.TempDir()
	installRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(installRoot, ".gitignore"), []byte(localManagedDirRelativePath+"\n"), stateFilePerm); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
	agentsDir := filepath.Join(installRoot, localAgentsRelativePath)
	if err := os.MkdirAll(agentsDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "existing.agent.md"), []byte("existing"), stateFilePerm); err != nil {
		t.Fatalf("seed existing agent: %v", err)
	}

	mcpPath := filepath.Join(installRoot, localMCPRelativePath)
	if err := os.MkdirAll(filepath.Dir(mcpPath), stateDirPerm); err != nil {
		t.Fatalf("create mcp parent dir: %v", err)
	}
	if err := writeJSONFile(mcpPath, map[string]any{
		"servers": map[string]any{
			mcpServerKey: map[string]any{
				"command": localMCPCommand,
			},
		},
	}); err != nil {
		t.Fatalf("seed mcp config: %v", err)
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	m := &Manager{
		now:     func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir: func() (string, error) { return home, nil },
	}
	if err := m.saveTrackedState(stateDir, trackedState{
		Version: trackedSchemaVersion,
		Local: []localInstall{
			{
				InstallRoot: installRoot,
				Mode:        localInstallModeTeam,
				BinaryOnly:  false,
				Repo:        releaseRepo,
				ReleaseID:   100,
				ReleaseTag:  "v0.9.0",
				InstalledAt: "2026-01-01T00:00:00Z",
				Managed: managedState{
					Files: []string{
						filepath.Join(installRoot, localManagedDirRelativePath, assetArtifactMCP),
						filepath.Join(installRoot, localManagedDirRelativePath, assetArtifactWeb),
						filepath.Join(installRoot, localAgentsRelativePath, "existing.agent.md"),
					},
					Dirs: []string{
						filepath.Join(installRoot, localManagedDirRelativePath),
						filepath.Join(installRoot, localAgentsRelativePath),
					},
				},
				JSONEdits: trackedJSONOpsFromEdits(nil, []mcpEdit{
					{
						File:    mcpPath,
						Key:     mcpServerKey,
						Touched: true,
					},
				}),
				IgnoreEdits: []ignoreEdit{
					{File: filepath.Join(installRoot, ".gitignore"), AddedLines: []string{localManagedDirRelativePath}},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	agentsArchive := zipBytes(t, map[string]string{"agents/new.agent.md": "new"})
	bundleArchive := zipBytes(t, map[string]string{
		assetArtifactMCP: "mcp-binary",
		assetArtifactWeb: "web-binary",
	})

	m = &Manager{
		httpClient:            successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive),
		now:                   func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:               func() (string, error) { return home, nil },
		workingDir:            func() (string, error) { return installRoot, nil },
		skipAttestationsCheck: true,
		promptIn:              strings.NewReader("2\n"),
		promptOut:             io.Discard,
		runCommand:            func(context.Context, string, ...string) ([]byte, error) { return []byte(installRoot + "\n"), nil },
		installBinary: func(src, dst string) error {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, binaryFilePerm)
		},
	}

	if err := m.installLocal(context.Background()); err != nil {
		t.Fatalf("installLocal should succeed: %v", err)
	}

	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		t.Fatalf("load tracked state: %v", err)
	}
	record, _ := state.localInstallForRoot(installRoot)
	if record == nil {
		t.Fatalf("expected tracked local install for %s", installRoot)
	}
	if record.BinaryOnly {
		t.Fatalf("expected binaryOnly=false for existing tracked install")
	}
	if !containsString(record.Managed.Files, filepath.Join(installRoot, localAgentsRelativePath, "new.agent.md")) {
		t.Fatalf("expected managed agent file to remain tracked, got %#v", record.Managed.Files)
	}
	if _, ok := record.JSONEdits.mcpEditForFile(mcpPath); !ok {
		t.Fatalf("expected tracked mcp edit for %s", mcpPath)
	}
}

func TestApplyLocalIgnoreRules_PersonalUsesGitInfoExclude(t *testing.T) {
	installRoot := t.TempDir()
	excludePath := filepath.Join(installRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), stateDirPerm); err != nil {
		t.Fatalf("create exclude dir: %v", err)
	}

	edits, err := applyLocalIgnoreRules(installRoot, installRoot, localInstallModePersonal)
	if err != nil {
		t.Fatalf("applyLocalIgnoreRules returned error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected exactly 1 ignore edit, got %d", len(edits))
	}

	wantPath := filepath.Join(installRoot, ".git", "info", "exclude")
	if filepath.Clean(edits[0].File) != wantPath {
		t.Fatalf("expected edit file %s, got %s", wantPath, edits[0].File)
	}

	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read exclude file: %v", err)
	}
	got := string(b)
	for _, want := range []string{localManagedDirRelativePath, filepath.ToSlash(filepath.Join(localAgentsRelativePath, "*.agent.md"))} {
		if !strings.Contains(got, want+"\n") {
			t.Fatalf("expected %q in exclude file, got %q", want, got)
		}
	}

	if _, err := os.Stat(filepath.Join(installRoot, ".gitignore")); err == nil {
		t.Fatalf("did not expect .gitignore to be created for personal mode")
	}
}

func TestApplyLocalIgnoreRules_TeamUsesGitignore(t *testing.T) {
	installRoot := t.TempDir()
	edits, err := applyLocalIgnoreRules(installRoot, installRoot, localInstallModeTeam)
	if err != nil {
		t.Fatalf("applyLocalIgnoreRules returned error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected exactly 1 ignore edit, got %d", len(edits))
	}

	wantPath := filepath.Join(installRoot, ".gitignore")
	if filepath.Clean(edits[0].File) != wantPath {
		t.Fatalf("expected edit file %s, got %s", wantPath, edits[0].File)
	}

	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, localManagedDirRelativePath+"\n") {
		t.Fatalf("expected %q in .gitignore, got %q", localManagedDirRelativePath, got)
	}

	if _, err := os.Stat(filepath.Join(installRoot, ".git", "info", "exclude")); err == nil {
		t.Fatalf("did not expect .git/info/exclude to be created for team mode")
	}
}

func TestApplyLocalIgnoreRules_PersonalUsesGitdirPointerExcludePath(t *testing.T) {
	repoRoot := t.TempDir()
	installRoot := filepath.Join(repoRoot, "nested", "workspace")
	if err := os.MkdirAll(installRoot, stateDirPerm); err != nil {
		t.Fatalf("create install root: %v", err)
	}

	worktreeGitDir := filepath.Join(repoRoot, ".worktrees", "nested")
	if err := os.MkdirAll(filepath.Join(worktreeGitDir, "info"), stateDirPerm); err != nil {
		t.Fatalf("create worktree gitdir info dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".git"), []byte("gitdir: .worktrees/nested\n"), stateFilePerm); err != nil {
		t.Fatalf("write .git pointer file: %v", err)
	}

	edits, err := applyLocalIgnoreRules(installRoot, repoRoot, localInstallModePersonal)
	if err != nil {
		t.Fatalf("applyLocalIgnoreRules returned error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected exactly 1 ignore edit, got %d", len(edits))
	}

	wantPath := filepath.Join(worktreeGitDir, "info", "exclude")
	if filepath.Clean(edits[0].File) != wantPath {
		t.Fatalf("expected edit file %s, got %s", wantPath, edits[0].File)
	}

	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read exclude file: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		"nested/workspace/.ccsubagents",
		"nested/workspace/.github/agents/*.agent.md",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Fatalf("expected %q in exclude file, got %q", want, got)
		}
	}
}

func TestApplyLocalIgnoreRules_TeamUsesRepoRootGitignoreForSubdirInstall(t *testing.T) {
	repoRoot := t.TempDir()
	installRoot := filepath.Join(repoRoot, "nested", "workspace")
	if err := os.MkdirAll(installRoot, stateDirPerm); err != nil {
		t.Fatalf("create install root: %v", err)
	}

	edits, err := applyLocalIgnoreRules(installRoot, repoRoot, localInstallModeTeam)
	if err != nil {
		t.Fatalf("applyLocalIgnoreRules returned error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected exactly 1 ignore edit, got %d", len(edits))
	}

	wantPath := filepath.Join(repoRoot, ".gitignore")
	if filepath.Clean(edits[0].File) != wantPath {
		t.Fatalf("expected edit file %s, got %s", wantPath, edits[0].File)
	}
	if !containsString(edits[0].AddedLines, "nested/workspace/.ccsubagents") {
		t.Fatalf("expected nested managed-dir rule in edits: %#v", edits[0].AddedLines)
	}

	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "nested/workspace/.ccsubagents\n") {
		t.Fatalf("expected nested managed-dir rule in .gitignore, got %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestReportGlobalPathWarning_WarnsWhenLocalBinMissing(t *testing.T) {
	var out bytes.Buffer
	home := filepath.Join(string(os.PathSeparator), "home", "user")

	m := &Manager{
		statusOut: &out,
		getenv: func(key string) string {
			if key == "PATH" {
				return "/usr/bin:/bin"
			}
			return ""
		},
	}

	m.reportGlobalPathWarning(home)
	got := out.String()
	if !strings.Contains(got, "⚠ ~/.local/bin is not in PATH") {
		t.Fatalf("expected PATH warning, got %q", got)
	}
	if !strings.Contains(got, "export PATH=\"$HOME/.local/bin:$PATH\"") {
		t.Fatalf("expected PATH export hint, got %q", got)
	}
}

func TestReportGlobalPathWarning_NoWarningWhenLocalBinPresent(t *testing.T) {
	var out bytes.Buffer
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	expected := filepath.Join(home, binaryInstallDirDefaultRel)

	m := &Manager{
		statusOut: &out,
		getenv: func(key string) string {
			if key == "PATH" {
				return strings.Join([]string{"/usr/bin", expected, "/bin"}, string(os.PathListSeparator))
			}
			return ""
		},
	}

	m.reportGlobalPathWarning(home)
	if got := out.String(); got != "" {
		t.Fatalf("expected no PATH warning when local bin is present, got %q", got)
	}
}
