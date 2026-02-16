package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	stateDirPerm               = 0o755
	stateFilePerm              = 0o644
	binaryFilePerm             = 0o755
	releaseRepo                = "CeraCharlesCC/CCSubAgents"
	releaseWorkflowPath        = ".github/workflows/manual-release.yml"
	releaseLatestURL           = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	assetAgentsZip             = "agents.zip"
	assetLocalArtifactZip      = "local-artifact.zip"
	assetArtifactMCP           = "local-artifact-mcp"
	assetArtifactWeb           = "local-artifact-web"
	binaryInstallDirDefaultRel = ".local/bin"
	binaryInstallDirEnv        = "LOCAL_ARTIFACT_BIN_DIR"
	trackedFileName            = "tracked.json"
	settingsRelativePath       = ".vscode-server-insiders/data/Machine/settings.json"
	settingsPathEnv            = "LOCAL_ARTIFACT_SETTINGS_PATH"
	mcpConfigRelativePath      = ".vscode-server-insiders/data/User/mcp.json"
	mcpConfigPathEnv           = "LOCAL_ARTIFACT_MCP_PATH"
	mcpServerKey               = "artifact-mcp"
	settingsAgentPathKey       = "chat.agentFilesLocations"
	agentsRelativePath         = ".local/share/ccsubagents/agents"
	trackedSchemaVersion       = 1
	installCommand             = "install"
	updateCommand              = "update"
	uninstallCommand           = "uninstall"
	httpsHeaderAccept          = "application/vnd.github+json"
	httpsHeaderUserAgent       = "ccsubagents-bootstrap"
	httpsHeaderAuthorization   = "Authorization"
	httpsHeaderGithubTokenPref = "Bearer "
	attestationOIDCIssuer      = "https://token.actions.githubusercontent.com"
)

var installAssetNames = []string{assetAgentsZip, assetLocalArtifactZip}

func (m *Manager) Run(ctx context.Context, command Command) error {
	switch command {
	case CommandInstall:
		return m.installOrUpdate(ctx, false)
	case CommandUpdate:
		return m.installOrUpdate(ctx, true)
	case CommandUninstall:
		return m.uninstall(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}

func (m *Manager) installOrUpdate(ctx context.Context, isUpdate bool) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	paths := resolveInstallPaths(home)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	previousState, err := m.loadTrackedStateForInstall(stateDir)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}

	assets, err := mapRequiredAssets(release.Assets, installAssetNames)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(stateDir, "download-*")
	if err != nil {
		return fmt.Errorf("create temp download dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	downloaded := map[string]string{}
	for _, name := range installAssetNames {
		asset := assets[name]
		dest := filepath.Join(tmpDir, name)
		if err := m.downloadFile(ctx, asset.BrowserDownloadURL, dest); err != nil {
			return fmt.Errorf("download release asset %q: %w", name, err)
		}
		downloaded[name] = dest
	}

	if err := m.verifyDownloadedAssets(ctx, downloaded); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	bundleDir := filepath.Join(tmpDir, "local-artifact")
	if err := os.MkdirAll(bundleDir, stateDirPerm); err != nil {
		return fmt.Errorf("create local-artifact bundle extraction dir: %w", err)
	}

	bundleBinaries, err := extractBundleBinaries(downloaded[assetLocalArtifactZip], bundleDir, []string{assetArtifactMCP, assetArtifactWeb})
	if err != nil {
		return fmt.Errorf("extract %s: %w", assetLocalArtifactZip, err)
	}

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

	for _, binaryName := range []string{assetArtifactMCP, assetArtifactWeb} {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := bundleBinaries[binaryName]
		dst := filepath.Join(paths.binaryDir, binaryName)
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

	settingsPath := paths.settingsPath
	if created, err := ensureParentDir(settingsPath); err != nil {
		return err
	} else if created {
		createdDirs = append(createdDirs, filepath.Dir(settingsPath))
		rollback.trackCreatedDir(filepath.Dir(settingsPath))
	}
	if err := rollback.captureFile(settingsPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	mcpPath := paths.mcpPath
	if created, err := ensureParentDir(mcpPath); err != nil {
		return err
	} else if created {
		createdDirs = append(createdDirs, filepath.Dir(mcpPath))
		rollback.trackCreatedDir(filepath.Dir(mcpPath))
	}
	if err := rollback.captureFile(mcpPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	settingsAgentPath := toHomeTildePath(home, agentsDir)
	mcpCommandPath := toHomeTildePath(home, filepath.Join(paths.binaryDir, assetArtifactMCP))

	if err := ctx.Err(); err != nil {
		return err
	}

	settingsEdit, err := applySettingsEdit(settingsPath, settingsAgentPath, previousState)
	if err != nil {
		return err
	}

	mcpEdit, err := applyMCPEdit(mcpPath, mcpCommandPath, previousState)
	if err != nil {
		return err
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
		JSONEdits: trackedJSONOps{
			Settings: settingsEdit,
			MCP:      mcpEdit,
		},
	}

	if isUpdate && previousState != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := removeStaleAgentFilesWithHook(previousState.Managed.Files, extractedFiles, agentsDir, rollback.captureFile); err != nil {
			return err
		}
	}

	if err := m.saveTrackedState(stateDir, state); err != nil {
		return err
	}

	return nil
}

func (m *Manager) uninstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	paths := resolveInstallPaths(home)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	state, err := m.loadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	agentsDir := filepath.Join(home, agentsRelativePath)
	settingsParentDir := filepath.Dir(paths.settingsPath)
	mcpParentDir := filepath.Dir(paths.mcpPath)
	allowedConfigParentDirs := []string{settingsParentDir, mcpParentDir}
	allowedBinaries := []string{
		filepath.Join(paths.binaryDir, assetArtifactMCP),
		filepath.Join(paths.binaryDir, assetArtifactWeb),
	}

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
	if err := revertSettingsEdit(state.JSONEdits.Settings); err != nil {
		return err
	}
	if err := revertMCPEdit(state.JSONEdits.MCP); err != nil {
		return err
	}

	dirs := append([]string{}, state.Managed.Dirs...)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
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
	if err := os.Remove(filepath.Join(stateDir, trackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove tracked state: %w", err)
	}

	return nil
}
