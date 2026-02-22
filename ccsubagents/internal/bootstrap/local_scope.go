package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type localScopeLocation struct {
	installRoot string
	repoRoot    string
	inGitRepo   bool
}

type localInstallConfig struct {
	isUpdate   bool
	location   localScopeLocation
	mode       localInstallMode
	binaryOnly bool
	stateDir   string
	state      *trackedState
	previous   *localInstall
}

func (m *Manager) installLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	state, err := m.loadTrackedStateForInstall(stateDir)
	if err != nil {
		return err
	}
	if state == nil {
		state = &trackedState{Version: trackedSchemaVersion}
	}

	location, err := m.resolveLocalScopeLocation(ctx, "install")
	if err != nil {
		return err
	}

	mode := localInstallModePersonal
	if location.inGitRepo {
		mode, err = m.promptLocalInstallMode(ctx)
		if err != nil {
			return err
		}
	}

	binaryOnly := false
	if mode == localInstallModeTeam && location.inGitRepo {
		detected, err := detectExistingTeamLocalSetup(location.installRoot, location.repoRoot)
		if err != nil {
			return err
		}
		binaryOnly = detected
	}

	var previous *localInstall
	if existing, _ := state.localInstallForRoot(location.installRoot); existing != nil {
		clone := *existing
		previous = &clone
	}

	cfg := localInstallConfig{
		isUpdate:   false,
		location:   location,
		mode:       mode,
		binaryOnly: binaryOnly,
		stateDir:   stateDir,
		state:      state,
		previous:   previous,
	}
	return m.installOrUpdateLocal(ctx, cfg)
}

func (m *Manager) updateLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	location, err := m.resolveLocalScopeLocation(ctx, "update")
	if err != nil {
		return err
	}

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.reportAction("No tracked local install found for %s", location.installRoot)
			return nil
		}
		return err
	}

	existing, _ := state.localInstallForRoot(location.installRoot)
	if existing == nil {
		m.reportAction("No tracked local install found for %s", location.installRoot)
		return nil
	}
	clone := *existing
	mode := clone.Mode
	if mode == "" {
		mode = localInstallModePersonal
	}

	cfg := localInstallConfig{
		isUpdate:   true,
		location:   location,
		mode:       mode,
		binaryOnly: clone.BinaryOnly,
		stateDir:   stateDir,
		state:      state,
		previous:   &clone,
	}
	return m.installOrUpdateLocal(ctx, cfg)
}

func (m *Manager) uninstallLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")

	location, err := m.resolveLocalScopeLocation(ctx, "uninstall")
	if err != nil {
		return err
	}

	m.reportPhase("Local Uninstall", "resolving environment")
	m.reportAction("Loading tracked installation state")
	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.reportAction("No tracked install found (nothing to uninstall)")
			return nil
		}
		return err
	}

	record, _ := state.localInstallForRoot(location.installRoot)
	if record == nil {
		m.reportAction("No tracked local install found for %s", location.installRoot)
		return nil
	}
	m.reportAction("Found tracked local installation")
	if record.Mode == localInstallModeTeam {
		if err := m.confirmTeamLocalUninstall(ctx, location.installRoot); err != nil {
			return err
		}
	}

	managedDir := filepath.Join(location.installRoot, localManagedDirRelativePath)
	agentsDir := filepath.Join(location.installRoot, localAgentsRelativePath)
	mcpPath := filepath.Join(location.installRoot, localMCPRelativePath)
	allowedBinaries := []string{
		filepath.Join(managedDir, assetArtifactMCP),
		filepath.Join(managedDir, assetArtifactWeb),
	}

	m.reportPhase("Local Uninstall", "removing managed files")
	m.reportAction("Removing %d tracked files", len(record.Managed.Files))
	for _, path := range record.Managed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if !isAllowedManagedPath(clean, agentsDir, allowedBinaries) {
			return fmt.Errorf("refusing to delete unsafe tracked path: %s", clean)
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", clean, err)
		}
	}

	m.reportPhase("Local Uninstall", "reverting configuration edits")
	for _, edit := range record.JSONEdits.allSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := revertSettingsEdit(edit); err != nil {
			return err
		}
	}
	for _, edit := range record.JSONEdits.allMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := revertMCPEdit(edit); err != nil {
			return err
		}
	}
	if err := revertIgnoreEdits(record.IgnoreEdits); err != nil {
		return err
	}

	allowedConfigParentDirs := []string{filepath.Dir(mcpPath), managedDir}
	dirs := append([]string{}, record.Managed.Dirs...)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	m.reportPhase("Local Uninstall", "cleaning managed directories")
	m.reportAction("Removing %d tracked directories", len(dirs))
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(dir)
		if !isAllowedManagedDirectory(clean, agentsDir, allowedConfigParentDirs) {
			return fmt.Errorf("refusing to delete unsafe tracked directory: %s", clean)
		}
		if err := os.Remove(clean); err != nil {
			if errors.Is(err, os.ErrNotExist) || isDirNotEmptyError(err) {
				continue
			}
			return fmt.Errorf("remove tracked directory %s: %w", clean, err)
		}
	}

	m.reportPhase("Local Uninstall", "finalizing")
	state.removeLocalInstall(location.installRoot)
	state.Version = trackedSchemaVersion
	if state.empty() {
		m.reportAction("Removing tracked state file")
		if err := os.Remove(filepath.Join(stateDir, trackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove tracked state: %w", err)
		}
	} else {
		m.reportAction("Saving tracked state")
		if err := m.saveTrackedState(stateDir, *state); err != nil {
			return err
		}
	}
	m.reportAction("Local uninstall complete")
	return nil
}

func (m *Manager) installOrUpdateLocal(ctx context.Context, cfg localInstallConfig) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	commandName := "Local Install"
	if cfg.isUpdate {
		commandName = "Local Update"
	}
	m.reportPhase(commandName, "resolving environment")
	m.reportAction("Install root: %s", cfg.location.installRoot)
	if cfg.binaryOnly {
		m.reportAction("Existing team setup detected; refreshing binaries only")
	}

	m.reportPhase(commandName, "fetching latest release metadata")
	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}

	requiredAssets := []string{assetLocalArtifactZip}
	if !cfg.binaryOnly {
		requiredAssets = append(requiredAssets, assetAgentsZip)
	}
	assets, err := mapRequiredAssets(release.Assets, requiredAssets)
	if err != nil {
		return err
	}
	m.reportAction("Using release %s", release.TagName)

	tmpDir, err := os.MkdirTemp(cfg.stateDir, "download-local-*")
	if err != nil {
		return fmt.Errorf("create temp download dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	m.reportPhase(commandName, "downloading required assets")
	downloaded := map[string]string{}
	for _, name := range requiredAssets {
		asset := assets[name]
		dest := filepath.Join(tmpDir, name)
		m.reportAction("Downloading %s", name)
		if err := m.downloadFile(ctx, asset.BrowserDownloadURL, dest); err != nil {
			return fmt.Errorf("download release asset %q: %w", name, err)
		}
		downloaded[name] = dest
	}

	m.reportPhase(commandName, "verifying attestations")
	if m.skipAttestationsCheck {
		m.reportAction("Skipping attestation verification (--skip-attestations-check)")
	} else {
		if err := m.verifyDownloadedAssets(ctx, downloaded); err != nil {
			return err
		}
	}

	m.reportPhase(commandName, "extracting bundles")
	bundleDir := filepath.Join(tmpDir, "local-artifact")
	if err := os.MkdirAll(bundleDir, stateDirPerm); err != nil {
		return fmt.Errorf("create local-artifact bundle extraction dir: %w", err)
	}

	bundleBinaries, err := extractBundleBinaries(downloaded[assetLocalArtifactZip], bundleDir, []string{assetArtifactMCP, assetArtifactWeb})
	if err != nil {
		return fmt.Errorf("extract %s: %w", assetLocalArtifactZip, err)
	}
	m.reportAction("Extracted %s", assetLocalArtifactZip)

	rollback := newInstallRollback()
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := rollback.restore(); rollbackErr != nil {
			retErr = fmt.Errorf("%w (rollback failed: %v)", retErr, rollbackErr)
		}
	}()

	managedDir := filepath.Join(cfg.location.installRoot, localManagedDirRelativePath)
	createdDirs := []string{}
	if created, err := ensureDirTracked(managedDir); err != nil {
		return err
	} else if created {
		createdDirs = append(createdDirs, managedDir)
		rollback.trackCreatedDir(managedDir)
	}

	binaryPaths := []string{
		filepath.Join(managedDir, assetArtifactMCP),
		filepath.Join(managedDir, assetArtifactWeb),
	}
	installer := m.installBinary
	if installer == nil {
		installer = installBinary
	}

	m.reportPhase(commandName, "installing binaries")
	for _, binaryName := range []string{assetArtifactMCP, assetArtifactWeb} {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := bundleBinaries[binaryName]
		dst := filepath.Join(managedDir, binaryName)
		m.reportAction("Installing %s", binaryName)
		if err := rollback.captureFile(dst); err != nil {
			return err
		}
		if err := installer(src, dst); err != nil {
			return fmt.Errorf("install %s into %s: %w", binaryName, managedDir, err)
		}
	}

	extractedFiles := []string{}
	extractedDirs := []string{}
	mcpEdits := []mcpEdit{}
	if !cfg.binaryOnly {
		agentsDir := filepath.Join(cfg.location.installRoot, localAgentsRelativePath)
		if created, err := ensureDirTracked(filepath.Dir(agentsDir)); err != nil {
			return err
		} else if created {
			createdDirs = append(createdDirs, filepath.Dir(agentsDir))
			rollback.trackCreatedDir(filepath.Dir(agentsDir))
		}
		if created, err := ensureDirTracked(agentsDir); err != nil {
			return err
		} else if created {
			createdDirs = append(createdDirs, agentsDir)
			rollback.trackCreatedDir(agentsDir)
		}

		m.reportPhase(commandName, "extracting agents")
		extractedFiles, extractedDirs, err = extractAgentsArchiveWithHook(downloaded[assetAgentsZip], agentsDir, rollback.captureFile)
		if err != nil {
			return fmt.Errorf("extract %s into %s: %w", assetAgentsZip, agentsDir, err)
		}
		m.reportAction("Extracted %s", assetAgentsZip)

		mcpPath := filepath.Join(cfg.location.installRoot, localMCPRelativePath)
		if created, err := ensureParentDir(mcpPath); err != nil {
			return err
		} else if created {
			createdDirs = append(createdDirs, filepath.Dir(mcpPath))
			rollback.trackCreatedDir(filepath.Dir(mcpPath))
		}
		if err := rollback.captureFile(mcpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		var previousState *trackedState
		if cfg.previous != nil {
			previousState = &trackedState{JSONEdits: cfg.previous.JSONEdits}
		}
		m.reportAction("Updating workspace MCP configuration")
		mcpEdit, err := applyMCPEdit(mcpPath, localMCPCommand, previousState)
		if err != nil {
			return err
		}
		mcpEdits = append(mcpEdits, mcpEdit)

		if cfg.isUpdate && cfg.previous != nil {
			m.reportPhase(commandName, "cleaning up stale managed agent files")
			m.reportAction("Removing stale managed agent files")
			if err := removeStaleAgentFilesWithHook(cfg.previous.Managed.Files, extractedFiles, agentsDir, rollback.captureFile); err != nil {
				return err
			}
		}
	}

	ignoreEdits := []ignoreEdit{}
	if cfg.isUpdate && cfg.previous != nil {
		ignoreEdits = mergeIgnoreEdits(ignoreEdits, cfg.previous.IgnoreEdits)
	}
	if !cfg.isUpdate && cfg.location.inGitRepo {
		m.reportAction("Updating repository ignore rules")
		ignoreFile, _, err := resolveLocalIgnoreTarget(cfg.location.installRoot, cfg.location.repoRoot, cfg.mode)
		if err != nil {
			return err
		}
		if ignoreFile != "" {
			if err := rollback.captureFile(ignoreFile); err != nil {
				return err
			}
		}
		newEdits, err := applyLocalIgnoreRules(cfg.location.installRoot, cfg.location.repoRoot, cfg.mode)
		if err != nil {
			return err
		}
		ignoreEdits = mergeIgnoreEdits(ignoreEdits, newEdits)
		if cfg.previous != nil {
			ignoreEdits = mergeIgnoreEdits(ignoreEdits, cfg.previous.IgnoreEdits)
		}
	}

	record := localInstall{
		InstallRoot: cfg.location.installRoot,
		Mode:        cfg.mode,
		BinaryOnly:  cfg.binaryOnly,
		Repo:        releaseRepo,
		ReleaseID:   release.ID,
		ReleaseTag:  release.TagName,
		InstalledAt: m.now().UTC().Format(time.RFC3339),
		Managed: managedState{
			Files: uniqueSorted(append(append([]string{}, binaryPaths...), extractedFiles...)),
			Dirs:  uniqueSorted(append(createdDirs, extractedDirs...)),
		},
		JSONEdits:   trackedJSONOpsFromEdits(nil, mcpEdits),
		IgnoreEdits: ignoreEdits,
	}

	cfg.state.Version = trackedSchemaVersion
	cfg.state.setLocalInstall(record)

	m.reportPhase(commandName, "finalizing installation state")
	m.reportAction("Saving tracked state")
	if err := m.saveTrackedState(cfg.stateDir, *cfg.state); err != nil {
		return err
	}
	if cfg.isUpdate {
		m.reportAction("Local update complete: %s", release.TagName)
	} else {
		m.reportAction("Local install complete: %s", release.TagName)
	}
	return nil
}

func (m *Manager) resolveLocalScopeLocation(ctx context.Context, action string) (localScopeLocation, error) {
	cwdFn := m.workingDir
	if cwdFn == nil {
		cwdFn = os.Getwd
	}
	cwd, err := cwdFn()
	if err != nil {
		return localScopeLocation{}, fmt.Errorf("determine current working directory: %w", err)
	}
	cwd = filepath.Clean(cwd)

	repoRoot, inRepo := m.detectGitRepoRoot(ctx, cwd)
	if !inRepo || repoRoot == "" || filepath.Clean(repoRoot) == cwd {
		return localScopeLocation{installRoot: cwd, inGitRepo: inRepo, repoRoot: repoRoot}, nil
	}

	input := m.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := m.promptOut
	if output == nil {
		output = os.Stdout
	}
	reader := bufio.NewReader(input)

	for {
		if err := ctx.Err(); err != nil {
			return localScopeLocation{}, err
		}
		if _, err := fmt.Fprintf(output, "Select local %s location:\n", action); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := fmt.Fprintf(output, "Current directory: %s\n", cwd); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := fmt.Fprintf(output, "Repository root: %s\n", repoRoot); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := io.WriteString(output, "This might not be the location you intended.\n"); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  1. Install here anyway\n"); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  2. Install at repository root\n"); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  3. Cancel\n"); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}
		if _, err := io.WriteString(output, "Enter choice [1-3]: "); err != nil {
			return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return localScopeLocation{}, fmt.Errorf("read local location selection: %w", err)
		}
		switch strings.TrimSpace(line) {
		case "1":
			return localScopeLocation{installRoot: cwd, inGitRepo: true, repoRoot: repoRoot}, nil
		case "2":
			return localScopeLocation{installRoot: repoRoot, inGitRepo: true, repoRoot: repoRoot}, nil
		case "3":
			return localScopeLocation{}, errors.New("local scope action canceled")
		default:
			if errors.Is(err, io.EOF) {
				return localScopeLocation{}, errors.New("local scope action canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter 1, 2, or 3.\n\n"); writeErr != nil {
				return localScopeLocation{}, fmt.Errorf("write local location prompt: %w", writeErr)
			}
		}
	}
}

func (m *Manager) promptLocalInstallMode(ctx context.Context) (localInstallMode, error) {
	input := m.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := m.promptOut
	if output == nil {
		output = os.Stdout
	}
	reader := bufio.NewReader(input)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := io.WriteString(output, "Choose local install usage mode:\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  1. Personal use\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  2. Team / project-wide use\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "Enter choice [1-2]: "); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read local mode selection: %w", err)
		}
		switch strings.TrimSpace(line) {
		case "1":
			return localInstallModePersonal, nil
		case "2":
			return localInstallModeTeam, nil
		default:
			if errors.Is(err, io.EOF) {
				return "", errors.New("local mode selection canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter 1 or 2.\n\n"); writeErr != nil {
				return "", fmt.Errorf("write local mode prompt: %w", writeErr)
			}
		}
	}
}

func (m *Manager) confirmTeamLocalUninstall(ctx context.Context, installRoot string) error {
	input := m.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := m.promptOut
	if output == nil {
		output = os.Stdout
	}
	reader := bufio.NewReader(input)

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Team local install at %s. Type YES to confirm uninstall: ", installRoot); err != nil {
		return fmt.Errorf("write team uninstall confirmation prompt: %w", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read team uninstall confirmation: %w", err)
	}
	if strings.TrimSpace(line) != "YES" {
		return errors.New("local uninstall canceled")
	}
	return nil
}

func (m *Manager) detectGitRepoRoot(ctx context.Context, cwd string) (string, bool) {
	runner := m.runCommand
	if runner == nil {
		runner = runCommand
	}
	out, err := runner(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return filepath.Clean(root), true
}

func localIgnorePathPrefix(installRoot, repoRoot string) string {
	if strings.TrimSpace(repoRoot) == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(installRoot))
	if err != nil {
		return ""
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" || rel == string(os.PathSeparator) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func resolveGitExcludePath(repoRoot string) (string, error) {
	gitPath := filepath.Join(filepath.Clean(repoRoot), ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("inspect git metadata path %s: %w", gitPath, err)
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "info", "exclude"), nil
	}

	b, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("read git metadata path %s: %w", gitPath, err)
	}
	line := strings.TrimSpace(string(b))
	const gitDirPrefix = "gitdir:"
	if !strings.HasPrefix(line, gitDirPrefix) {
		return "", fmt.Errorf("unsupported git metadata format in %s", gitPath)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, gitDirPrefix))
	if gitDir == "" {
		return "", fmt.Errorf("missing gitdir in %s", gitPath)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(gitPath), gitDir)
	}
	return filepath.Join(filepath.Clean(gitDir), "info", "exclude"), nil
}

func resolveLocalIgnoreTarget(installRoot, repoRoot string, mode localInstallMode) (string, []string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = installRoot
	}
	prefix := localIgnorePathPrefix(installRoot, repoRoot)
	toRepoPath := func(rel string) string {
		if prefix == "" {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(filepath.Join(prefix, rel))
	}

	switch mode {
	case localInstallModePersonal:
		excludePath, err := resolveGitExcludePath(repoRoot)
		if err != nil {
			return "", nil, err
		}
		lines := []string{
			toRepoPath(localManagedDirRelativePath),
			toRepoPath(filepath.Join(localAgentsRelativePath, "*.agent.md")),
		}
		return excludePath, lines, nil
	case localInstallModeTeam:
		gitIgnorePath := filepath.Join(repoRoot, ".gitignore")
		lines := []string{toRepoPath(localManagedDirRelativePath)}
		return gitIgnorePath, lines, nil
	default:
		return "", nil, nil
	}
}

func applyLocalIgnoreRules(installRoot, repoRoot string, mode localInstallMode) ([]ignoreEdit, error) {
	path, lines, err := resolveLocalIgnoreTarget(installRoot, repoRoot, mode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" || len(lines) == 0 {
		return nil, nil
	}

	added, err := appendMissingIgnoreLines(path, lines)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return nil, nil
	}
	return []ignoreEdit{{File: path, AddedLines: added}}, nil
}

func mergeIgnoreEdits(base []ignoreEdit, next []ignoreEdit) []ignoreEdit {
	out := append([]ignoreEdit{}, base...)
	for _, edit := range next {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		merged := false
		for idx := range out {
			if filepath.Clean(out[idx].File) != filepath.Clean(edit.File) {
				continue
			}
			out[idx].AddedLines = uniqueSorted(append(out[idx].AddedLines, edit.AddedLines...))
			merged = true
			break
		}
		if !merged {
			copyEdit := ignoreEdit{File: edit.File, AddedLines: uniqueSorted(edit.AddedLines)}
			out = append(out, copyEdit)
		}
	}
	return out
}

func appendMissingIgnoreLines(path string, lines []string) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(path), stateDirPerm); err != nil {
		return nil, fmt.Errorf("create ignore parent directory %s: %w", filepath.Dir(path), err)
	}

	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read ignore file %s: %w", path, err)
	}

	existingSet := map[string]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(existing, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		existingSet[trimmed] = struct{}{}
	}

	added := []string{}
	builder := existing
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, ok := existingSet[trimmed]; ok {
			continue
		}
		if builder != "" && !strings.HasSuffix(builder, "\n") {
			builder += "\n"
		}
		builder += trimmed + "\n"
		existingSet[trimmed] = struct{}{}
		added = append(added, trimmed)
	}
	if len(added) == 0 {
		return nil, nil
	}
	if err := os.WriteFile(path, []byte(builder), stateFilePerm); err != nil {
		return nil, fmt.Errorf("write ignore file %s: %w", path, err)
	}
	return added, nil
}

func revertIgnoreEdits(edits []ignoreEdit) error {
	for _, edit := range edits {
		if strings.TrimSpace(edit.File) == "" || len(edit.AddedLines) == 0 {
			continue
		}
		if err := removeIgnoreLines(edit.File, edit.AddedLines); err != nil {
			return err
		}
	}
	return nil
}

func removeIgnoreLines(path string, lines []string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read ignore file %s for uninstall: %w", path, err)
	}

	toRemove := map[string]int{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		toRemove[trimmed]++
	}
	if len(toRemove) == 0 {
		return nil
	}

	normalized := strings.ReplaceAll(string(b), "\r\n", "\n")
	parts := strings.Split(normalized, "\n")
	kept := make([]string, 0, len(parts))
	removedAny := false
	for _, line := range parts {
		trimmed := strings.TrimSpace(line)
		if remaining := toRemove[trimmed]; remaining > 0 {
			toRemove[trimmed] = remaining - 1
			removedAny = true
			continue
		}
		kept = append(kept, line)
	}
	if !removedAny {
		return nil
	}

	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), stateFilePerm); err != nil {
		return fmt.Errorf("write ignore file %s during uninstall: %w", path, err)
	}
	return nil
}

func detectExistingTeamLocalSetup(installRoot, repoRoot string) (bool, error) {
	ignorePath, lines, err := resolveLocalIgnoreTarget(installRoot, repoRoot, localInstallModeTeam)
	if err != nil {
		return false, err
	}
	if ignorePath == "" || len(lines) == 0 {
		return false, nil
	}

	gitIgnoreHasManagedDir, err := ignoreFileHasLine(ignorePath, lines[0])
	if err != nil {
		return false, err
	}
	if !gitIgnoreHasManagedDir {
		return false, nil
	}

	agentsExist, err := managedAgentFilesExist(filepath.Join(installRoot, localAgentsRelativePath))
	if err != nil {
		return false, err
	}
	if !agentsExist {
		return false, nil
	}

	mcpHasLocalCommand, err := localMCPCommandConfigured(filepath.Join(installRoot, localMCPRelativePath))
	if err != nil {
		return false, err
	}
	return mcpHasLocalCommand, nil
}

func ignoreFileHasLine(path, expected string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == expected {
			return true, nil
		}
	}
	return false, nil
}

func managedAgentFilesExist(agentsDir string) (bool, error) {
	info, err := os.Stat(agentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", agentsDir, err)
	}
	if !info.IsDir() {
		return false, nil
	}

	found := false
	err = filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".agent.md") {
			found = true
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("walk %s: %w", agentsDir, err)
	}
	return found, nil
}

func localMCPCommandConfigured(path string) (bool, error) {
	root, err := readJSONFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	serversRaw, ok := root["servers"]
	if !ok {
		return false, nil
	}
	servers, ok := serversRaw.(map[string]any)
	if !ok {
		return false, nil
	}
	serverRaw, ok := servers[mcpServerKey]
	if !ok {
		return false, nil
	}
	server, ok := serverRaw.(map[string]any)
	if !ok {
		return false, nil
	}
	command, _ := server["command"].(string)
	return strings.TrimSpace(command) == localMCPCommand, nil
}

func (m *Manager) reportGlobalPathWarning(home string) {
	getenv := m.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	expected := filepath.Clean(filepath.Join(home, binaryInstallDirDefaultRel))
	for _, entry := range filepath.SplitList(getenv("PATH")) {
		if filepath.Clean(strings.TrimSpace(entry)) == expected {
			return
		}
	}
	m.reportAction("Warning: %s is not in PATH", toHomeTildePath(home, expected))
	m.reportAction("Add it to your shell profile, e.g.: export PATH=\"$HOME/.local/bin:$PATH\"")
}
