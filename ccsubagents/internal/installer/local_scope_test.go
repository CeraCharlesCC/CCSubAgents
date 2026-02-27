package installer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
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
	if err := os.Mkdir(filepath.Join(stateDir, state.TrackedFileName), stateDirPerm); err != nil {
		t.Fatalf("create tracked-state collision dir: %v", err)
	}

	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, bundleBinaryFiles("mcp-binary", "web-binary"))

	m := &Runner{
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
		mode:     state.LocalInstallModePersonal,
		stateDir: stateDir,
		state:    &state.TrackedState{Version: state.TrackedSchemaVersion},
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

func TestInstallOrUpdateLocal_AttestationFailureUpdateReportsActionableGuidance(t *testing.T) {
	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, bundleBinaryFiles("mcp-binary", "web-binary"))

	var out bytes.Buffer
	m := &Runner{
		httpClient: successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive),
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("verification failed")
		},
		statusOut: &out,
	}

	err := m.installOrUpdateLocal(context.Background(), localInstallConfig{
		isUpdate: true,
		location: localScopeLocation{
			installRoot: installRoot,
			repoRoot:    installRoot,
		},
		mode:     state.LocalInstallModePersonal,
		stateDir: stateDir,
		state:    &state.TrackedState{Version: state.TrackedSchemaVersion},
	})
	if err == nil {
		t.Fatalf("expected attestation verification error")
	}
	for _, want := range []string{
		"Error: attestation verification failed for agents.zip",
		"To skip verification: ccsubagents update --scope=local --skip-attestations-check",
		"(not recommended for production use)",
		"verification failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to include %q, got %v", want, err)
		}
	}

	status := out.String()
	for _, want := range []string{
		"ccsubagents v1.2.3",
		"✓ Checked local install root",
		"✓ Downloaded release assets (v1.2.3)",
		"✗ Verified attestations",
		"Failed asset: agents.zip",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected status output to include %q, got:\n%s", want, status)
		}
	}
}

func TestInstallLocal_TeamRerunPreservesFullManagedState(t *testing.T) {
	home := t.TempDir()
	installRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(installRoot, ".gitignore"), []byte(config.LocalManagedDirRelativePath+"\n"), stateFilePerm); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
	agentsDir := filepath.Join(installRoot, config.LocalAgentsRelativePath)
	if err := os.MkdirAll(agentsDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "existing.agent.md"), []byte("existing"), stateFilePerm); err != nil {
		t.Fatalf("seed existing agent: %v", err)
	}

	mcpPath := filepath.Join(installRoot, config.LocalMCPRelativePath)
	if err := os.MkdirAll(filepath.Dir(mcpPath), stateDirPerm); err != nil {
		t.Fatalf("create mcp parent dir: %v", err)
	}
	if err := writeJSONMap(mcpPath, map[string]any{
		"servers": map[string]any{
			config.MCPServerKey: map[string]any{
				"command": config.LocalMCPCommand(runtime.GOOS),
			},
		},
	}); err != nil {
		t.Fatalf("seed mcp config: %v", err)
	}

	stateDir := globalStateDirForTest(home)
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	mcpBinaryName, webBinaryName := localArtifactBinaryNames(runtime.GOOS)
	m := &Runner{
		now:     func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir: func() (string, error) { return home, nil },
	}
	if err := state.SaveTrackedState(stateDir, state.TrackedState{
		Version: state.TrackedSchemaVersion,
		Local: []state.LocalInstall{
			{
				InstallRoot: installRoot,
				Mode:        state.LocalInstallModeTeam,
				BinaryOnly:  false,
				Repo:        release.Repo,
				ReleaseID:   100,
				ReleaseTag:  "v0.9.0",
				InstalledAt: "2026-01-01T00:00:00Z",
				Managed: state.ManagedState{
					Files: []string{
						filepath.Join(installRoot, config.LocalManagedDirRelativePath, mcpBinaryName),
						filepath.Join(installRoot, config.LocalManagedDirRelativePath, webBinaryName),
						filepath.Join(installRoot, config.LocalAgentsRelativePath, "existing.agent.md"),
					},
					Dirs: []string{
						filepath.Join(installRoot, config.LocalManagedDirRelativePath),
						filepath.Join(installRoot, config.LocalAgentsRelativePath),
					},
				},
				JSONEdits: state.TrackedJSONOpsFromEdits(nil, []state.MCPEdit{
					{
						File:    mcpPath,
						Key:     config.MCPServerKey,
						Touched: true,
					},
				}),
				IgnoreEdits: []state.IgnoreEdit{
					{File: filepath.Join(installRoot, ".gitignore"), AddedLines: []string{config.LocalManagedDirRelativePath}},
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	agentsArchive := zipBytes(t, map[string]string{"agents/new.agent.md": "new"})
	bundleArchive := zipBytes(t, bundleBinaryFiles("mcp-binary", "web-binary"))

	m = &Runner{
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

	tracked, err := state.LoadTrackedState(stateDir)
	if err != nil {
		t.Fatalf("load tracked state: %v", err)
	}
	record, _ := tracked.LocalInstallForRoot(installRoot)
	if record == nil {
		t.Fatalf("expected tracked local install for %s", installRoot)
	}
	if record.BinaryOnly {
		t.Fatalf("expected binaryOnly=false for existing tracked install")
	}
	if !containsString(record.Managed.Files, filepath.Join(installRoot, config.LocalAgentsRelativePath, "new.agent.md")) {
		t.Fatalf("expected managed agent file to remain tracked, got %#v", record.Managed.Files)
	}
	if _, ok := record.JSONEdits.MCPEditForFile(mcpPath); !ok {
		t.Fatalf("expected tracked mcp edit for %s", mcpPath)
	}
}

func TestApplyLocalIgnoreRules_PersonalUsesGitInfoExclude(t *testing.T) {
	installRoot := t.TempDir()
	excludePath := filepath.Join(installRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), stateDirPerm); err != nil {
		t.Fatalf("create exclude dir: %v", err)
	}

	edits, err := config.ApplyLocalIgnoreRules(installRoot, installRoot, state.LocalInstallModePersonal, stateDirPerm, stateFilePerm)
	if err != nil {
		t.Fatalf("config.ApplyLocalIgnoreRules returned error: %v", err)
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
	for _, want := range []string{config.LocalManagedDirRelativePath, filepath.ToSlash(filepath.Join(config.LocalAgentsRelativePath, "*.agent.md"))} {
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
	edits, err := config.ApplyLocalIgnoreRules(installRoot, installRoot, state.LocalInstallModeTeam, stateDirPerm, stateFilePerm)
	if err != nil {
		t.Fatalf("config.ApplyLocalIgnoreRules returned error: %v", err)
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
	if !strings.Contains(got, config.LocalManagedDirRelativePath+"\n") {
		t.Fatalf("expected %q in .gitignore, got %q", config.LocalManagedDirRelativePath, got)
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

	edits, err := config.ApplyLocalIgnoreRules(installRoot, repoRoot, state.LocalInstallModePersonal, stateDirPerm, stateFilePerm)
	if err != nil {
		t.Fatalf("config.ApplyLocalIgnoreRules returned error: %v", err)
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

	edits, err := config.ApplyLocalIgnoreRules(installRoot, repoRoot, state.LocalInstallModeTeam, stateDirPerm, stateFilePerm)
	if err != nil {
		t.Fatalf("config.ApplyLocalIgnoreRules returned error: %v", err)
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

	m := &Runner{
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
	if !strings.Contains(got, "⚠ "+toHomeTildePath(home, filepath.Join(home, binaryInstallDirDefaultRel))+" is not in PATH") {
		t.Fatalf("expected PATH warning, got %q", got)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(got, "Add it to your user PATH environment variable.") {
			t.Fatalf("expected windows PATH hint, got %q", got)
		}
	} else if !strings.Contains(got, "export PATH=\"$HOME/.local/bin:$PATH\"") {
		t.Fatalf("expected PATH export hint, got %q", got)
	}
}

func TestReportGlobalPathWarning_NoWarningWhenLocalBinPresent(t *testing.T) {
	var out bytes.Buffer
	home := filepath.Join(string(os.PathSeparator), "home", "user")
	expected := filepath.Join(home, binaryInstallDirDefaultRel)

	m := &Runner{
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
