package installer

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

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

type localScopeLocation struct {
	installRoot string
	repoRoot    string
	inGitRepo   bool
}

type localInstallConfig struct {
	isUpdate   bool
	location   localScopeLocation
	mode       state.LocalInstallMode
	binaryOnly bool
	stateDir   string
	state      *state.TrackedState
	previous   *state.LocalInstall
}

func (r *Runner) localTrackedStateDir() (string, error) {
	home, err := r.homeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "ccsubagents"), nil
}

func (r *Runner) installLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, err := r.localTrackedStateDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	tracked, err := state.LoadTrackedStateForInstall(stateDir)
	if err != nil {
		return err
	}
	if tracked == nil {
		tracked = &state.TrackedState{Version: state.TrackedSchemaVersion}
	}

	location, err := r.resolveLocalScopeLocation(ctx, "install")
	if err != nil {
		return err
	}

	mode := state.LocalInstallModePersonal
	if location.inGitRepo {
		mode, err = r.promptLocalInstallMode(ctx)
		if err != nil {
			return err
		}
	}

	var previous *state.LocalInstall
	if existing, _ := tracked.LocalInstallForRoot(location.installRoot); existing != nil {
		clone := *existing
		previous = &clone
	}

	binaryOnly := false
	if mode == state.LocalInstallModeTeam && location.inGitRepo {
		if previous != nil {
			binaryOnly = previous.BinaryOnly
		} else {
			detected, err := config.DetectExistingTeamLocalSetup(location.installRoot, location.repoRoot)
			if err != nil {
				return err
			}
			binaryOnly = detected
		}
	}

	cfg := localInstallConfig{
		isUpdate:   false,
		location:   location,
		mode:       mode,
		binaryOnly: binaryOnly,
		stateDir:   stateDir,
		state:      tracked,
		previous:   previous,
	}
	return r.installOrUpdateLocal(ctx, cfg)
}

func (r *Runner) updateLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, err := r.localTrackedStateDir()
	if err != nil {
		return err
	}

	location, err := r.resolveLocalScopeLocation(ctx, "update")
	if err != nil {
		return err
	}

	tracked, err := state.LoadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.reportStepOK("Checked tracked local installation", fmt.Sprintf("none found for %s", filepath.ToSlash(location.installRoot)))
			return nil
		}
		return err
	}

	existing, _ := tracked.LocalInstallForRoot(location.installRoot)
	if existing == nil {
		r.reportStepOK("Checked tracked local installation", fmt.Sprintf("none found for %s", filepath.ToSlash(location.installRoot)))
		return nil
	}
	clone := *existing
	mode := clone.Mode
	if mode == "" {
		mode = state.LocalInstallModePersonal
	}

	cfg := localInstallConfig{
		isUpdate:   true,
		location:   location,
		mode:       mode,
		binaryOnly: clone.BinaryOnly,
		stateDir:   stateDir,
		state:      tracked,
		previous:   &clone,
	}
	return r.installOrUpdateLocal(ctx, cfg)
}

func (r *Runner) uninstallLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, err := r.localTrackedStateDir()
	if err != nil {
		return err
	}

	location, err := r.resolveLocalScopeLocation(ctx, "uninstall")
	if err != nil {
		return err
	}

	tracked, err := state.LoadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.reportStepOK("Checked tracked local installation", fmt.Sprintf("none found for %s", filepath.ToSlash(location.installRoot)))
			return nil
		}
		return err
	}

	record, _ := tracked.LocalInstallForRoot(location.installRoot)
	if record == nil {
		r.reportStepOK("Checked tracked local installation", fmt.Sprintf("none found for %s", filepath.ToSlash(location.installRoot)))
		return nil
	}

	if strings.TrimSpace(record.ReleaseTag) == "" {
		r.reportStepOK("Checked tracked local installation", fmt.Sprintf("found for %s", filepath.ToSlash(location.installRoot)))
	} else {
		r.reportStepOK("Checked tracked local installation", fmt.Sprintf("%s found for %s", record.ReleaseTag, filepath.ToSlash(location.installRoot)))
	}

	if record.Mode == state.LocalInstallModeTeam {
		if err := r.confirmTeamLocalUninstall(ctx, location.installRoot); err != nil {
			return err
		}
	}

	managedDir := filepath.Join(location.installRoot, config.LocalManagedDirRelativePath)
	agentsDir := filepath.Join(location.installRoot, config.LocalAgentsRelativePath)
	mcpPath := filepath.Join(location.installRoot, config.LocalMCPRelativePath)
	allowedBinaries := []string{
		filepath.Join(managedDir, assetArtifactMCP),
		filepath.Join(managedDir, assetArtifactWeb),
	}

	for _, path := range record.Managed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if !files.IsAllowedManagedPath(clean, agentsDir, allowedBinaries) {
			return fmt.Errorf("refusing to delete unsafe tracked path: %s", clean)
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", clean, err)
		}
	}
	r.reportStepOK("Removed managed local files", "")

	for _, edit := range record.JSONEdits.AllSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := config.RevertSettingsEdit(edit, stateFilePerm); err != nil {
			return err
		}
	}
	for _, edit := range record.JSONEdits.AllMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := config.RevertMCPEdit(edit, stateFilePerm); err != nil {
			return err
		}
	}
	if err := config.RevertIgnoreEdits(record.IgnoreEdits, stateFilePerm); err != nil {
		return err
	}
	r.reportStepOK("Reverted local configuration edits", "")

	allowedConfigParentDirs := []string{filepath.Dir(mcpPath), managedDir}
	dirs := append([]string{}, record.Managed.Dirs...)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(dir)
		if !files.IsAllowedManagedDirectory(clean, agentsDir, allowedConfigParentDirs) {
			return fmt.Errorf("refusing to delete unsafe tracked directory: %s", clean)
		}
		if err := os.Remove(clean); err != nil {
			if errors.Is(err, os.ErrNotExist) || files.IsDirNotEmptyError(err) {
				continue
			}
			return fmt.Errorf("remove tracked directory %s: %w", clean, err)
		}
	}
	r.reportStepOK("Removed managed local directories", "")

	tracked.RemoveLocalInstall(location.installRoot)
	tracked.Version = state.TrackedSchemaVersion
	if tracked.Empty() {
		if err := os.Remove(filepath.Join(stateDir, state.TrackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove tracked state: %w", err)
		}
		r.reportStepOK("Updated tracked installation state", "removed")
	} else {
		if err := state.SaveTrackedState(stateDir, *tracked); err != nil {
			return err
		}
		r.reportStepOK("Updated tracked installation state", "saved")
	}
	r.reportCompletion("Local uninstall")
	return nil
}

func (r *Runner) installOrUpdateLocal(ctx context.Context, cfg localInstallConfig) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		r.pendingPinWrite = nil
	}()

	previousSettingsRoot := r.installSettingsRoot
	r.installSettingsRoot = cfg.location.installRoot
	defer func() {
		r.installSettingsRoot = previousSettingsRoot
	}()

	var (
		rel release.Response
		err error
	)
	if cfg.isUpdate {
		rel, err = r.resolveReleaseForUpdate(ctx)
	} else {
		rel, err = r.resolveReleaseForInstall(ctx)
	}
	if err != nil {
		return err
	}
	r.reportVersionHeader(rel.TagName)
	r.reportStepOK("Checked local install root", filepath.ToSlash(cfg.location.installRoot))
	if cfg.binaryOnly {
		r.reportStepOK("Detected existing team setup", "refreshing binaries only")
	}

	requiredAssets := []string{assetLocalArtifactZip}
	if !cfg.binaryOnly {
		requiredAssets = append(requiredAssets, assetAgentsZip)
	}
	tmpDir, downloaded, err := r.downloadRequiredAssets(ctx, cfg.stateDir, rel, requiredAssets, "download-local-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := r.verifyAttestationsOrReport(ctx, downloaded, cfg.isUpdate, ScopeLocal); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	bundleBinaries, err := r.extractDownloadedBundle(tmpDir, downloaded)
	if err != nil {
		return err
	}

	rollback := files.NewRollback()
	mutations := files.NewMutationTracker(rollback, stateDirPerm)
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := rollback.Restore(); rollbackErr != nil {
			retErr = fmt.Errorf("%w (rollback failed: %v)", retErr, rollbackErr)
		}
	}()

	managedDir := filepath.Join(cfg.location.installRoot, config.LocalManagedDirRelativePath)
	if err := mutations.EnsureDir(managedDir); err != nil {
		return err
	}

	binaryPaths, err := r.installExtractedBinaries(ctx, bundleBinaries, managedDir, mutations, "")
	if err != nil {
		return err
	}
	r.reportStepOK("Installed local binaries", fmt.Sprintf("→ %s", filepath.ToSlash(managedDir)))

	extractedFiles := []string{}
	extractedDirs := []string{}
	mcpEdits := []state.MCPEdit{}
	if !cfg.binaryOnly {
		agentsDir := filepath.Join(cfg.location.installRoot, config.LocalAgentsRelativePath)
		if err := mutations.EnsureDir(filepath.Dir(agentsDir)); err != nil {
			return err
		}
		if err := mutations.EnsureDir(agentsDir); err != nil {
			return err
		}

		extractedFiles, extractedDirs, err = files.ExtractAgentsArchiveWithHook(downloaded[assetAgentsZip], agentsDir, mutations.SnapshotFile, stateDirPerm, stateFilePerm)
		if err != nil {
			return fmt.Errorf("extract %s into %s: %w", assetAgentsZip, agentsDir, err)
		}
		r.reportStepOK("Installed local agent definitions", fmt.Sprintf("→ %s", filepath.ToSlash(agentsDir)))
		r.reportDetail("extracted %d local agent definitions", len(extractedFiles))

		mcpPath := filepath.Join(cfg.location.installRoot, config.LocalMCPRelativePath)
		if err := mutations.EnsureParentDir(mcpPath); err != nil {
			return err
		}
		if err := mutations.SnapshotFile(mcpPath); err != nil {
			return err
		}

		var previousState *state.TrackedState
		if cfg.previous != nil {
			previousState = &state.TrackedState{JSONEdits: cfg.previous.JSONEdits}
		}
		mcpEdit, err := config.ApplyMCPEdit(mcpPath, config.LocalMCPCommand, previousState, stateFilePerm)
		if err != nil {
			return err
		}
		mcpEdits = append(mcpEdits, mcpEdit)
		r.reportStepOK("Updated workspace MCP configuration", "")
		r.reportDetail("updated workspace MCP config: %s", mcpPath)

		if cfg.isUpdate && cfg.previous != nil {
			if err := files.RemoveStaleAgentFilesWithHook(cfg.previous.Managed.Files, extractedFiles, agentsDir, mutations.SnapshotFile); err != nil {
				return err
			}
			r.reportStepOK("Removed stale managed local agent files", "")
		}
	} else {
		r.reportDetail("skipped local agent definitions and workspace MCP update (binary-only mode)")
	}

	ignoreEdits := []state.IgnoreEdit{}
	if cfg.isUpdate && cfg.previous != nil {
		ignoreEdits = config.MergeIgnoreEdits(ignoreEdits, cfg.previous.IgnoreEdits)
	}
	if !cfg.isUpdate && cfg.location.inGitRepo {
		ignoreFile, _, err := config.ResolveLocalIgnoreTarget(cfg.location.installRoot, cfg.location.repoRoot, cfg.mode)
		if err != nil {
			return err
		}
		if ignoreFile != "" {
			if err := mutations.SnapshotFile(ignoreFile); err != nil {
				return err
			}
		}
		newEdits, err := config.ApplyLocalIgnoreRules(cfg.location.installRoot, cfg.location.repoRoot, cfg.mode, stateDirPerm, stateFilePerm)
		if err != nil {
			return err
		}
		ignoreEdits = config.MergeIgnoreEdits(ignoreEdits, newEdits)
		if cfg.previous != nil {
			ignoreEdits = config.MergeIgnoreEdits(ignoreEdits, cfg.previous.IgnoreEdits)
		}
		r.reportStepOK("Updated repository ignore rules", "")
		if strings.TrimSpace(ignoreFile) != "" {
			r.reportDetail("updated ignore rules: %s", ignoreFile)
		}
	}

	record := state.LocalInstall{
		InstallRoot: cfg.location.installRoot,
		Mode:        cfg.mode,
		BinaryOnly:  cfg.binaryOnly,
		Repo:        release.Repo,
		ReleaseID:   rel.ID,
		ReleaseTag:  rel.TagName,
		InstalledAt: r.now().UTC().Format(time.RFC3339),
		Managed: state.ManagedState{
			Files: files.UniqueSorted(append(append([]string{}, binaryPaths...), extractedFiles...)),
			Dirs:  files.UniqueSorted(append(mutations.CreatedDirectories(), extractedDirs...)),
		},
		JSONEdits:   state.TrackedJSONOpsFromEdits(nil, mcpEdits),
		IgnoreEdits: ignoreEdits,
	}

	if err := r.persistPendingPinWrite(mutations); err != nil {
		return err
	}

	cfg.state.Version = state.TrackedSchemaVersion
	cfg.state.SetLocalInstall(record)

	if err := state.SaveTrackedState(cfg.stateDir, *cfg.state); err != nil {
		return err
	}
	r.reportStepOK("Updated tracked installation state", "saved")
	r.reportDetail("saved tracked state: %s", filepath.Join(cfg.stateDir, state.TrackedFileName))
	if cfg.isUpdate {
		r.reportCompletion("Local update")
	} else {
		r.reportCompletion("Local install")
	}
	return nil
}

func (r *Runner) resolveLocalScopeLocation(ctx context.Context, action string) (localScopeLocation, error) {
	cwdFn := r.workingDir
	if cwdFn == nil {
		cwdFn = os.Getwd
	}
	cwd, err := cwdFn()
	if err != nil {
		return localScopeLocation{}, fmt.Errorf("determine current working directory: %w", err)
	}
	cwd = filepath.Clean(cwd)

	repoRoot, inRepo := r.detectGitRepoRoot(ctx, cwd)
	if !inRepo || repoRoot == "" || filepath.Clean(repoRoot) == cwd {
		return localScopeLocation{installRoot: cwd, inGitRepo: inRepo, repoRoot: repoRoot}, nil
	}

	input := r.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := r.promptOut
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

func (r *Runner) detectGitRepoRoot(ctx context.Context, cwd string) (string, bool) {
	runner := r.runCommand
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

func (r *Runner) reportGlobalPathWarning(home string) {
	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	expected := filepath.Clean(filepath.Join(home, binaryInstallDirDefaultRel))
	for _, entry := range filepath.SplitList(getenv("PATH")) {
		if filepath.Clean(strings.TrimSpace(entry)) == expected {
			return
		}
	}
	r.reportWarning(
		fmt.Sprintf("%s is not in PATH", toHomeTildePath(home, expected)),
		"Add it to your shell profile:",
		"export PATH=\"$HOME/.local/bin:$PATH\"",
	)
}
