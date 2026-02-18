package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	stateDirPerm                  = 0o755
	stateFilePerm                 = 0o644
	binaryFilePerm                = 0o755
	releaseRepo                   = "CeraCharlesCC/CCSubAgents"
	releaseWorkflowPath           = ".github/workflows/manual-release.yml"
	releaseLatestURL              = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	assetAgentsZip                = "agents.zip"
	assetLocalArtifactZip         = "local-artifact.zip"
	assetArtifactMCP              = "local-artifact-mcp"
	assetArtifactWeb              = "local-artifact-web"
	binaryInstallDirDefaultRel    = ".local/bin"
	binaryInstallDirEnv           = "LOCAL_ARTIFACT_BIN_DIR"
	trackedFileName               = "tracked.json"
	settingsStableRelativePath    = ".vscode-server/data/Machine/settings.json"
	settingsInsidersRelativePath  = ".vscode-server-insiders/data/Machine/settings.json"
	settingsRelativePath          = settingsInsidersRelativePath
	settingsPathEnv               = "LOCAL_ARTIFACT_SETTINGS_PATH"
	mcpConfigStableRelativePath   = ".vscode-server/data/User/mcp.json"
	mcpConfigInsidersRelativePath = ".vscode-server-insiders/data/User/mcp.json"
	mcpConfigRelativePath         = mcpConfigInsidersRelativePath
	mcpConfigPathEnv              = "LOCAL_ARTIFACT_MCP_PATH"
	mcpServerKey                  = "artifact-mcp"
	settingsAgentPathKey          = "chat.agentFilesLocations"
	agentsRelativePath            = ".local/share/ccsubagents/agents"
	trackedSchemaVersion          = 1
	installCommand                = "install"
	updateCommand                 = "update"
	uninstallCommand              = "uninstall"
	httpsHeaderAccept             = "application/vnd.github+json"
	httpsHeaderUserAgent          = "ccsubagents-bootstrap"
	httpsHeaderAuthorization      = "Authorization"
	httpsHeaderGithubTokenPref    = "Bearer "
	attestationOIDCIssuer         = "https://token.actions.githubusercontent.com"
)

var installAssetNames = []string{assetAgentsZip, assetLocalArtifactZip}

type installDestination string

const (
	installDestinationStable   installDestination = "stable"
	installDestinationInsiders installDestination = "insiders"
	installDestinationBoth     installDestination = "both"
)

func (m *Manager) promptInstallDestination(ctx context.Context) (installDestination, error) {
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
		if _, err := io.WriteString(output, "Select install destination:\n"); err != nil {
			return "", fmt.Errorf("write install destination prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  1. .vscode-server\n"); err != nil {
			return "", fmt.Errorf("write install destination prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  2. .vscode-server-insiders\n"); err != nil {
			return "", fmt.Errorf("write install destination prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  3. both\n"); err != nil {
			return "", fmt.Errorf("write install destination prompt: %w", err)
		}
		if _, err := io.WriteString(output, "Enter choice [1-3]: "); err != nil {
			return "", fmt.Errorf("write install destination prompt: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read install destination selection: %w", err)
		}

		switch strings.TrimSpace(line) {
		case "1":
			return installDestinationStable, nil
		case "2":
			return installDestinationInsiders, nil
		case "3":
			return installDestinationBoth, nil
		default:
			if errors.Is(err, io.EOF) {
				return "", errors.New("install destination selection canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter 1, 2, or 3.\n\n"); writeErr != nil {
				return "", fmt.Errorf("write install destination prompt: %w", writeErr)
			}
		}
	}
}

func (m *Manager) Run(ctx context.Context, command Command) error {
	switch command {
	case CommandInstall:
		destination, err := m.promptInstallDestination(ctx)
		if err != nil {
			return err
		}
		m.installDestination = destination
		return m.installOrUpdate(ctx, false)
	case CommandUpdate:
		m.installDestination = ""
		return m.installOrUpdate(ctx, true)
	case CommandUninstall:
		m.installDestination = ""
		return m.uninstall(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}

func (m *Manager) reportPhase(commandName, phase string) {
	m.statusf("==> %s: %s\n", commandName, phase)
}

func (m *Manager) reportAction(format string, args ...any) {
	m.statusf("  - %s\n", fmt.Sprintf(format, args...))
}

func commandNameForInstallOrUpdate(isUpdate bool) string {
	if isUpdate {
		return "Update"
	}
	return "Install"
}

func resolveInstallTargets(paths installPaths, destination installDestination) ([]installConfigTarget, error) {
	switch destination {
	case installDestinationStable:
		return []installConfigTarget{{settingsPath: paths.stable.settingsPath, mcpPath: paths.stable.mcpPath}}, nil
	case installDestinationInsiders:
		return []installConfigTarget{{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath}}, nil
	case installDestinationBoth:
		return uniqueInstallTargets([]installConfigTarget{
			{settingsPath: paths.stable.settingsPath, mcpPath: paths.stable.mcpPath},
			{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath},
		}), nil
	default:
		return nil, fmt.Errorf("invalid install destination %q", destination)
	}
}

func resolveUpdateTargets(paths installPaths, previous *trackedState) []installConfigTarget {
	if previous == nil {
		return []installConfigTarget{{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath}}
	}

	settingsEdits := previous.JSONEdits.allSettingsEdits()
	mcpEdits := previous.JSONEdits.allMCPEdits()
	count := len(settingsEdits)
	if len(mcpEdits) > count {
		count = len(mcpEdits)
	}

	if count == 0 {
		return []installConfigTarget{{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath}}
	}

	targets := make([]installConfigTarget, 0, count)
	for idx := 0; idx < count; idx++ {
		target := installConfigTarget{}
		if idx < len(settingsEdits) {
			target.settingsPath = settingsEdits[idx].File
		}
		if idx < len(mcpEdits) {
			target.mcpPath = mcpEdits[idx].File
		}
		if strings.TrimSpace(target.settingsPath) == "" {
			target.settingsPath = paths.insiders.settingsPath
		}
		if strings.TrimSpace(target.mcpPath) == "" {
			target.mcpPath = paths.insiders.mcpPath
		}
		targets = append(targets, target)
	}

	return uniqueInstallTargets(targets)
}

func uniqueInstallTargets(targets []installConfigTarget) []installConfigTarget {
	out := make([]installConfigTarget, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		settingsPath := strings.TrimSpace(target.settingsPath)
		mcpPath := strings.TrimSpace(target.mcpPath)
		if settingsPath == "" || mcpPath == "" {
			continue
		}
		key := filepath.Clean(settingsPath) + "\n" + filepath.Clean(mcpPath)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, installConfigTarget{
			settingsPath: filepath.Clean(settingsPath),
			mcpPath:      filepath.Clean(mcpPath),
		})
	}
	return out
}

func (m *Manager) installOrUpdate(ctx context.Context, isUpdate bool) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	commandName := commandNameForInstallOrUpdate(isUpdate)
	m.reportPhase(commandName, "resolving environment")

	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	paths := resolveInstallPaths(home)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	m.reportAction("Loading tracked installation state")
	previousState, err := m.loadTrackedStateForInstall(stateDir)
	if err != nil {
		return err
	}
	if previousState == nil {
		m.reportAction("No existing tracked installation found")
	} else {
		m.reportAction("Found existing tracked installation")
	}

	var configTargets []installConfigTarget
	if isUpdate {
		configTargets = resolveUpdateTargets(paths, previousState)
	} else {
		destination := m.installDestination
		if destination == "" {
			destination = installDestinationInsiders
		}
		configTargets, err = resolveInstallTargets(paths, destination)
		if err != nil {
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	m.reportPhase(commandName, "fetching latest release metadata")
	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}

	assets, err := mapRequiredAssets(release.Assets, installAssetNames)
	if err != nil {
		return err
	}
	m.reportAction("Using release %s", release.TagName)

	tmpDir, err := os.MkdirTemp(stateDir, "download-*")
	if err != nil {
		return fmt.Errorf("create temp download dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	m.reportPhase(commandName, "downloading required assets")
	downloaded := map[string]string{}
	for _, name := range installAssetNames {
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
	if err := ctx.Err(); err != nil {
		return err
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

	agentsDir := filepath.Join(home, agentsRelativePath)
	createdDirs := []string{}
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

	binaryPaths := []string{
		filepath.Join(paths.binaryDir, assetArtifactMCP),
		filepath.Join(paths.binaryDir, assetArtifactWeb),
	}

	if err := os.MkdirAll(paths.binaryDir, stateDirPerm); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("create binary install directory %s: %w (requires privileges to write %s)", paths.binaryDir, err, paths.binaryDir)
		}
		return fmt.Errorf("create binary install directory %s: %w", paths.binaryDir, err)
	}

	installer := m.installBinary
	if installer == nil {
		installer = installBinary
	}
	m.reportPhase(commandName, "installing binaries and updating configuration")

	for _, binaryName := range []string{assetArtifactMCP, assetArtifactWeb} {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := bundleBinaries[binaryName]
		dst := filepath.Join(paths.binaryDir, binaryName)
		m.reportAction("Installing %s", binaryName)
		if err := rollback.captureFile(dst); err != nil {
			return err
		}
		if err := installer(src, dst); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("install %s into %s: %w (requires privileges to write %s)", binaryName, paths.binaryDir, err, paths.binaryDir)
			}
			return fmt.Errorf("install %s into %s: %w", binaryName, paths.binaryDir, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	extractedFiles, extractedDirs, err := extractAgentsArchiveWithHook(downloaded[assetAgentsZip], agentsDir, rollback.captureFile)
	if err != nil {
		return fmt.Errorf("extract %s into %s: %w", assetAgentsZip, agentsDir, err)
	}
	m.reportAction("Extracted %s", assetAgentsZip)

	for _, target := range configTargets {
		if created, err := ensureParentDir(target.settingsPath); err != nil {
			return err
		} else if created {
			createdDirs = append(createdDirs, filepath.Dir(target.settingsPath))
			rollback.trackCreatedDir(filepath.Dir(target.settingsPath))
		}
		if err := rollback.captureFile(target.settingsPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}

		if created, err := ensureParentDir(target.mcpPath); err != nil {
			return err
		} else if created {
			createdDirs = append(createdDirs, filepath.Dir(target.mcpPath))
			rollback.trackCreatedDir(filepath.Dir(target.mcpPath))
		}
		if err := rollback.captureFile(target.mcpPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}

	settingsAgentPath := toHomeTildePath(home, agentsDir)
	mcpCommandPath := toHomeTildePath(home, filepath.Join(paths.binaryDir, assetArtifactMCP))

	if err := ctx.Err(); err != nil {
		return err
	}
	m.reportAction("Updating settings and MCP configuration")

	settingsEdits := make([]settingsEdit, 0, len(configTargets))
	mcpEdits := make([]mcpEdit, 0, len(configTargets))
	for _, target := range configTargets {
		settingsEdit, err := applySettingsEdit(target.settingsPath, settingsAgentPath, previousState)
		if err != nil {
			return err
		}
		settingsEdits = append(settingsEdits, settingsEdit)

		mcpEdit, err := applyMCPEdit(target.mcpPath, mcpCommandPath, previousState)
		if err != nil {
			return err
		}
		mcpEdits = append(mcpEdits, mcpEdit)
	}

	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   release.ID,
		ReleaseTag:  release.TagName,
		InstalledAt: m.now().UTC().Format(time.RFC3339),
		Managed: managedState{
			Files: uniqueSorted(append(append([]string{}, binaryPaths...), extractedFiles...)),
			Dirs:  uniqueSorted(append(createdDirs, extractedDirs...)),
		},
		JSONEdits: trackedJSONOpsFromEdits(settingsEdits, mcpEdits),
	}

	if isUpdate {
		m.reportPhase(commandName, "cleaning up stale managed agent files")
		if previousState == nil {
			m.reportAction("No previously tracked files; skipping cleanup")
		} else {
			if err := ctx.Err(); err != nil {
				return err
			}
			m.reportAction("Removing stale managed agent files")
			if err := removeStaleAgentFilesWithHook(previousState.Managed.Files, extractedFiles, agentsDir, rollback.captureFile); err != nil {
				return err
			}
		}
	}

	m.reportPhase(commandName, "finalizing installation state")
	m.reportAction("Saving tracked state")
	if err := m.saveTrackedState(stateDir, state); err != nil {
		return err
	}
	if isUpdate {
		m.reportAction("Update complete: %s", release.TagName)
	} else {
		m.reportAction("Install complete: %s", release.TagName)
	}

	return nil
}

func (m *Manager) uninstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.reportPhase("Uninstall", "resolving environment")

	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	paths := resolveInstallPaths(home)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	m.reportAction("Loading tracked installation state")
	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.reportAction("No tracked install found (nothing to uninstall)")
			return nil
		}
		return err
	}
	m.reportAction("Found tracked installation")

	agentsDir := filepath.Join(home, agentsRelativePath)
	allowedConfigParentDirs := []string{
		filepath.Dir(paths.stable.settingsPath),
		filepath.Dir(paths.stable.mcpPath),
		filepath.Dir(paths.insiders.settingsPath),
		filepath.Dir(paths.insiders.mcpPath),
	}
	for _, edit := range state.JSONEdits.allSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		allowedConfigParentDirs = append(allowedConfigParentDirs, filepath.Dir(edit.File))
	}
	for _, edit := range state.JSONEdits.allMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		allowedConfigParentDirs = append(allowedConfigParentDirs, filepath.Dir(edit.File))
	}
	allowedConfigParentDirs = uniqueSorted(allowedConfigParentDirs)
	allowedBinaries := []string{
		filepath.Join(paths.binaryDir, assetArtifactMCP),
		filepath.Join(paths.binaryDir, assetArtifactWeb),
	}

	m.reportPhase("Uninstall", "removing managed files")
	m.reportAction("Removing %d tracked files", len(state.Managed.Files))
	for _, path := range state.Managed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if !isAllowedManagedPath(clean, agentsDir, allowedBinaries) {
			return fmt.Errorf("refusing to delete unsafe tracked path: %s", clean)
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("remove %s: %w (requires privileges for %s)", clean, err, paths.binaryDir)
			}
			return fmt.Errorf("remove %s: %w", clean, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	m.reportPhase("Uninstall", "reverting configuration edits")
	m.reportAction("Reverting settings and MCP configuration")
	for _, edit := range state.JSONEdits.allSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := revertSettingsEdit(edit); err != nil {
			return err
		}
	}
	for _, edit := range state.JSONEdits.allMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := revertMCPEdit(edit); err != nil {
			return err
		}
	}

	dirs := append([]string{}, state.Managed.Dirs...)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	m.reportPhase("Uninstall", "cleaning managed directories")
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

	if err := ctx.Err(); err != nil {
		return err
	}
	m.reportPhase("Uninstall", "finalizing")
	m.reportAction("Removing tracked state file")
	if err := os.Remove(filepath.Join(stateDir, trackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove tracked state: %w", err)
	}
	m.reportAction("Uninstall complete")

	return nil
}
