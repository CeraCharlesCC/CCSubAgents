package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/integration/txn"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/versiontag"
)

type pendingPinWrite struct {
	path       string
	scope      config.SettingsScope
	versionTag string
}

func (r *Runner) releaseClient() *release.Client {
	return &release.Client{
		HTTPClient: r.httpClient,
		LookPath:   r.lookPath,
		RunCommand: r.runCommand,
		Getenv:     r.getenv,
	}
}

func (r *Runner) resolveReleaseForInstall(ctx context.Context) (release.Response, error) {
	if err := ctx.Err(); err != nil {
		return release.Response{}, err
	}
	r.pendingPinWrite = nil

	home, settingsRoot, settings, err := r.resolveInstallSettingsContext()
	if err != nil {
		return release.Response{}, err
	}

	requestedTag := config.NormalizeVersionTag(r.installVersionRaw)
	pinnedTag := config.NormalizeVersionTag(settings.PinnedVersion)

	effectiveTag := requestedTag
	if pinnedTag != "" {
		if requestedTag == "" {
			return release.Response{}, fmt.Errorf("install is pinned to %s; rerun with --version %s or edit settings.json to change/remove pinned-version", pinnedTag, pinnedTag)
		}
		if requestedTag != pinnedTag {
			return release.Response{}, fmt.Errorf("install is pinned to %s; requested --version %s does not match", pinnedTag, requestedTag)
		}
		effectiveTag = pinnedTag
	}
	if r.pinRequested && effectiveTag == "" {
		return release.Response{}, ErrPinnedRequiresVersion
	}

	rel, err := r.fetchReleaseForVersion(ctx, effectiveTag)
	if err != nil {
		return release.Response{}, err
	}

	if !r.pinRequested {
		return rel, nil
	}

	pinPath, pinScope, err := config.ChoosePinWritePath(settingsRoot, home)
	if err != nil {
		return release.Response{}, fmt.Errorf("determine pin settings path: %w", err)
	}
	r.pendingPinWrite = &pendingPinWrite{
		path:       pinPath,
		scope:      pinScope,
		versionTag: effectiveTag,
	}

	return rel, nil
}

func (r *Runner) resolveReleaseForUpdate(ctx context.Context) (release.Response, error) {
	if err := ctx.Err(); err != nil {
		return release.Response{}, err
	}
	r.pendingPinWrite = nil

	_, _, settings, err := r.resolveInstallSettingsContext()
	if err != nil {
		return release.Response{}, err
	}

	pinnedTag := config.NormalizeVersionTag(settings.PinnedVersion)
	if pinnedTag != "" {
		return release.Response{}, fmt.Errorf("update is blocked because pinned-version is set to %s; edit settings.json to clear pinned-version before updating", pinnedTag)
	}

	return r.fetchReleaseForVersion(ctx, "")
}

func (r *Runner) fetchReleaseForVersion(ctx context.Context, tag string) (release.Response, error) {
	requestedTag := config.NormalizeVersionTag(tag)
	client := r.releaseClient()
	if requestedTag == "" {
		return client.FetchLatest(ctx)
	}

	rel, err := client.FetchByTag(ctx, requestedTag)
	if err == nil {
		return rel, nil
	}

	if errors.Is(err, release.ErrReleaseNotFound) {
		r.reportWarning(
			"Requested version does not exist",
			fmt.Sprintf("Version %s was not found in %s.", requestedTag, release.Repo),
		)
		return release.Response{}, fmt.Errorf("requested version %s was not found", requestedTag)
	}

	return release.Response{}, err
}

func (r *Runner) resolveInstallSettingsContext() (home, settingsRoot string, settings config.InstallSettings, err error) {
	homeDir := r.homeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err = homeDir()
	if err != nil {
		return "", "", config.InstallSettings{}, fmt.Errorf("determine home directory: %w", err)
	}

	settingsRoot = strings.TrimSpace(r.installSettingsRoot)
	if settingsRoot == "" {
		workingDir := r.workingDir
		if workingDir == nil {
			workingDir = os.Getwd
		}
		settingsRoot, err = workingDir()
		if err != nil {
			return "", "", config.InstallSettings{}, fmt.Errorf("determine current working directory: %w", err)
		}
	}
	settingsRoot = filepath.Clean(settingsRoot)

	settings, err = config.LoadMergedInstallSettings(home, settingsRoot)
	if err != nil {
		return "", "", config.InstallSettings{}, fmt.Errorf("load install settings: %w", err)
	}

	return home, settingsRoot, settings, nil
}

func (r *Runner) persistPendingPinWrite(mutations *files.MutationTracker) error {
	if r.pendingPinWrite == nil {
		return nil
	}

	pending := *r.pendingPinWrite
	if mutations != nil {
		if err := mutations.EnsureParentDirWithinBase(pending.path, filepath.Dir(pending.path)); err != nil {
			return err
		}
		if err := mutations.SnapshotFile(pending.path); err != nil {
			return err
		}
	}

	if err := config.WritePinnedVersion(pending.path, pending.versionTag, stateDirPerm, stateFilePerm); err != nil {
		return err
	}
	r.reportDetail("saved pinned-version %s to %s settings (%s)", pending.versionTag, pending.scope, pending.path)
	r.pendingPinWrite = nil
	return nil
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

func resolveUpdateTargets(paths installPaths, previous *state.TrackedState) []installConfigTarget {
	if previous == nil {
		return []installConfigTarget{{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath}}
	}

	settingsEdits := previous.JSONEdits.AllSettingsEdits()
	mcpEdits := previous.JSONEdits.AllMCPEdits()
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

func (r *Runner) downloadRequiredAssets(ctx context.Context, stateDir string, rel release.Response, requiredAssetNames []string, tempPrefix string) (string, map[string]string, error) {
	resolvedAssets := rel.Assets
	missing := missingRequiredAssetNames(resolvedAssets, requiredAssetNames)
	if len(missing) > 0 {
		companion, err := r.fetchCompanionReleaseForMissingAssets(ctx, rel, missing)
		if err != nil {
			return "", nil, err
		}
		if len(companion.Assets) > 0 {
			resolvedAssets = mergeReleaseAssets(resolvedAssets, companion.Assets)
		}
	}

	assets, err := release.MapRequiredAssets(resolvedAssets, requiredAssetNames)
	if err != nil {
		return "", nil, err
	}

	tmpDir, err := os.MkdirTemp(stateDir, tempPrefix)
	if err != nil {
		return "", nil, fmt.Errorf("create temp download dir: %w", err)
	}

	client := r.releaseClient()
	downloaded := map[string]string{}
	for _, name := range requiredAssetNames {
		asset := assets[name]
		dest := filepath.Join(tmpDir, name)
		if err := client.DownloadFile(ctx, asset.BrowserDownloadURL, dest, stateFilePerm); err != nil {
			if removeErr := os.RemoveAll(tmpDir); removeErr != nil {
				_ = removeErr
			}
			return "", nil, fmt.Errorf("download release asset %q: %w", name, err)
		}
		downloaded[name] = dest
		if info, statErr := os.Stat(dest); statErr == nil {
			r.reportDetail("downloaded %s (%d bytes)", name, info.Size())
		}
	}

	r.reportStepOK("Downloaded release assets", rel.TagName)
	return tmpDir, downloaded, nil
}

func (r *Runner) fetchCompanionReleaseForMissingAssets(ctx context.Context, rel release.Response, missing []string) (release.Response, error) {
	needsLocalArtifactRelease := false
	for _, name := range missing {
		if isLocalArtifactBundleAsset(name) {
			needsLocalArtifactRelease = true
			break
		}
	}
	if !needsLocalArtifactRelease {
		return release.Response{}, nil
	}

	mainTag := versiontag.Normalize(rel.TagName)
	if mainTag == "" {
		return release.Response{}, nil
	}

	companionTag := localArtifactTagPrefix + mainTag
	companion, err := r.releaseClient().FetchByExactTag(ctx, companionTag)
	if err == nil {
		return companion, nil
	}
	if errors.Is(err, release.ErrReleaseNotFound) {
		return release.Response{}, nil
	}
	return release.Response{}, fmt.Errorf("fetch release %s: %w", companionTag, err)
}

func missingRequiredAssetNames(assets []release.Asset, required []string) []string {
	byName := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		byName[asset.Name] = struct{}{}
	}

	missing := make([]string, 0, len(required))
	for _, name := range required {
		if _, ok := byName[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}

func mergeReleaseAssets(primary, secondary []release.Asset) []release.Asset {
	merged := make([]release.Asset, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	appendUnique := func(src []release.Asset) {
		for _, asset := range src {
			if _, ok := seen[asset.Name]; ok {
				continue
			}
			seen[asset.Name] = struct{}{}
			merged = append(merged, asset)
		}
	}
	appendUnique(primary)
	appendUnique(secondary)
	return merged
}

func isLocalArtifactBundleAsset(name string) bool {
	return strings.HasPrefix(name, "local-artifact_") && strings.HasSuffix(name, ".zip")
}

func (r *Runner) verifyAttestationsOrReport(ctx context.Context, downloaded map[string]string, isUpdate bool, scope Scope) error {
	if r.skipAttestationsCheck {
		r.reportStepOK("Verified attestations", "skipped (--skip-attestations-check)")
		r.reportDetail("attestation verification skipped by flag")
		return nil
	}

	if err := r.releaseClient().VerifyDownloadedAssets(ctx, downloaded, r.reportDetail); err != nil {
		var attestationErr *release.AttestationVerificationError
		if errors.As(err, &attestationErr) {
			r.reportStepFail("Verified attestations")
			r.reportMessageLine("Failed asset: %s", attestationErr.Asset)
			return formatAttestationVerificationFailure(attestationErr, commandForAttestationSkip(isUpdate, scope))
		}
		return err
	}

	r.reportStepOK("Verified attestations", "")
	return nil
}

func (r *Runner) extractDownloadedBundle(tmpDir string, downloaded map[string]string, goos, goarch string) (map[string]string, error) {
	bundleAssetName := localArtifactBundleAssetName(goos, goarch)
	bundleZipPath := downloaded[bundleAssetName]
	if bundleZipPath == "" {
		return nil, fmt.Errorf("missing downloaded release asset %q", bundleAssetName)
	}

	bundleDir := filepath.Join(tmpDir, "local-artifact")
	if err := os.MkdirAll(bundleDir, stateDirPerm); err != nil {
		return nil, fmt.Errorf("create local-artifact bundle extraction dir: %w", err)
	}
	mcpBinaryName, webBinaryName := localArtifactBinaryNames(goos)
	daemonBinaryName := ccsubagentsdBinaryName(goos)

	bundleBinaries, err := files.ExtractBundleBinaries(bundleZipPath, bundleDir, []string{mcpBinaryName, webBinaryName}, binaryFilePerm)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", bundleAssetName, err)
	}
	if daemonBinary, daemonErr := files.ExtractBundleBinaries(bundleZipPath, bundleDir, []string{daemonBinaryName}, binaryFilePerm); daemonErr == nil {
		for name, path := range daemonBinary {
			bundleBinaries[name] = path
		}
	} else if !strings.Contains(strings.ToLower(daemonErr.Error()), "archive missing required file") {
		return nil, fmt.Errorf("extract %s: %w", bundleAssetName, daemonErr)
	}
	r.reportDetail("extracted bundle %s", bundleAssetName)
	return bundleBinaries, nil
}

type fileSnapshotter interface {
	SnapshotFile(string) error
}

func (r *Runner) installExtractedBinaries(ctx context.Context, bundleBinaries map[string]string, destinationDir string, mutations fileSnapshotter, permissionHintDir, goos string) ([]string, error) {
	mcpBinaryName, webBinaryName := localArtifactBinaryNames(goos)
	binaryNames := []string{mcpBinaryName, webBinaryName}
	daemonBinaryName := ccsubagentsdBinaryName(goos)
	if _, ok := bundleBinaries[daemonBinaryName]; ok {
		binaryNames = append(binaryNames, daemonBinaryName)
	}

	binaryPaths := []string{
		filepath.Join(destinationDir, mcpBinaryName),
		filepath.Join(destinationDir, webBinaryName),
	}
	if len(binaryNames) > len(binaryPaths) {
		for _, binaryName := range binaryNames[len(binaryPaths):] {
			binaryPaths = append(binaryPaths, filepath.Join(destinationDir, binaryName))
		}
	}

	installer := r.installBinary
	if installer == nil {
		installer = func(src, dst string) error {
			return files.InstallBinaryWithinBase(src, dst, destinationDir, binaryFilePerm)
		}
	}

	for _, binaryName := range binaryNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		src := bundleBinaries[binaryName]
		dst := filepath.Join(destinationDir, binaryName)
		if err := mutations.SnapshotFile(dst); err != nil {
			return nil, err
		}
		if err := installer(src, dst); err != nil {
			if permissionHintDir != "" && errors.Is(err, os.ErrPermission) {
				return nil, fmt.Errorf("install %s into %s: %w (requires privileges to write %s)", binaryName, destinationDir, err, permissionHintDir)
			}
			return nil, fmt.Errorf("install %s into %s: %w", binaryName, destinationDir, err)
		}
		r.reportDetail("installed binary: %s", binaryName)
	}

	return binaryPaths, nil
}

func (r *Runner) installOrUpdate(ctx context.Context, isUpdate bool) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		r.pendingPinWrite = nil
	}()

	home, err := r.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	layout := paths.Global(home)
	if stateOverride := strings.TrimSpace(getenv(paths.EnvStateDir)); stateOverride != "" {
		layout.StateDir = filepath.Clean(stateOverride)
	}
	if blobOverride := strings.TrimSpace(getenv(paths.EnvBlobDir)); blobOverride != "" {
		layout.BlobDir = filepath.Clean(blobOverride)
	}
	paths := resolveInstallPaths(home)

	stateDir := layout.StateDir
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	previousState, err := state.LoadTrackedStateForInstall(stateDir)
	if err != nil {
		return err
	}
	previousGlobal := previousState.GlobalInstallSnapshot()

	var configTargets []installConfigTarget
	if isUpdate {
		configTargets = resolveUpdateTargets(paths, previousGlobal)
	} else {
		if len(r.globalInstallTargets) > 0 {
			configTargets = uniqueInstallTargets(r.globalInstallTargets)
		} else {
			destination := r.installDestination
			if destination == "" {
				destination = installDestinationInsiders
			}
			configTargets, err = resolveInstallTargets(paths, destination)
			if err != nil {
				return err
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var rel release.Response
	if isUpdate {
		rel, err = r.resolveReleaseForUpdate(ctx)
	} else {
		rel, err = r.resolveReleaseForInstall(ctx)
	}
	if err != nil {
		return err
	}
	if isUpdate && previousGlobal != nil && rel.TagName == previousGlobal.ReleaseTag {
		r.statusf("ccsubagents: already at latest version (%s). Nothing to do.\n", rel.TagName)
		return nil
	}

	r.reportVersionHeader(rel.TagName)
	if previousGlobal == nil {
		r.reportStepOK("Checked for existing installation", "none found")
	} else {
		r.reportStepOK("Checked for existing installation", fmt.Sprintf("%s found", previousGlobal.ReleaseTag))
	}
	r.reportDetail("tracked state path: %s", filepath.Join(stateDir, state.TrackedFileName))

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	tmpDir, downloaded, err := r.downloadRequiredAssets(ctx, stateDir, rel, installAssetNamesForRuntime(), "download-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.RemoveAll(tmpDir); removeErr != nil {
			_ = removeErr
		}
	}()

	if err := r.verifyAttestationsOrReport(ctx, downloaded, isUpdate, ScopeGlobal); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	bundleBinaries, err := r.extractDownloadedBundle(tmpDir, downloaded, goos, goarch)
	if err != nil {
		return err
	}

	txSession, err := txn.Begin(stateDir, layout.BlobDir, "global", commandNameForInstallOrUpdate(isUpdate), []string{"mutations"})
	if err != nil {
		return err
	}
	defer txSession.Close()
	if err := txSession.MarkApplied("mutations"); err != nil {
		return err
	}

	rollback := txSession.NewRollback("mutations")
	mutations := files.NewMutationTracker(rollback, stateDirPerm)
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := rollback.Restore(); rollbackErr != nil {
			retErr = fmt.Errorf("%w (rollback failed: %v)", retErr, rollbackErr)
		}
		if txRollbackErr := txSession.Rollback(); txRollbackErr != nil {
			retErr = fmt.Errorf("%w (transaction rollback failed: %v)", retErr, txRollbackErr)
		}
	}()

	agentsDir := filepath.Join(layout.StateDir, "agents")
	if err := mutations.EnsureDirWithinBase(filepath.Dir(agentsDir), layout.StateDir); err != nil {
		return err
	}
	if err := mutations.EnsureDirWithinBase(agentsDir, layout.StateDir); err != nil {
		return err
	}

	if err := os.MkdirAll(paths.binaryDir, stateDirPerm); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("create binary install directory %s: %w (requires privileges to write %s)", paths.binaryDir, err, paths.binaryDir)
		}
		return fmt.Errorf("create binary install directory %s: %w", paths.binaryDir, err)
	}

	binaryPaths, err := r.installExtractedBinaries(ctx, bundleBinaries, paths.binaryDir, mutations, paths.binaryDir, goos)
	if err != nil {
		return err
	}
	r.reportStepOK("Installed binaries", fmt.Sprintf("→ %s", toHomeTildePath(home, paths.binaryDir)))

	if err := ctx.Err(); err != nil {
		return err
	}

	extractedFiles, extractedDirs, err := files.ExtractAgentsArchiveWithHook(downloaded[assetAgentsZip], agentsDir, mutations.SnapshotFile, stateDirPerm, stateFilePerm)
	if err != nil {
		return fmt.Errorf("extract %s into %s: %w", assetAgentsZip, agentsDir, err)
	}
	r.reportStepOK("Installed agent definitions", fmt.Sprintf("→ %s", toHomeTildePath(home, agentsDir)))
	r.reportDetail("extracted %d agent definitions", len(extractedFiles))

	for _, target := range configTargets {
		if err := mutations.EnsureParentDirWithinBase(target.settingsPath, filepath.Dir(target.settingsPath)); err != nil {
			return err
		}
		if err := mutations.SnapshotFile(target.settingsPath); err != nil {
			return err
		}

		if err := mutations.EnsureParentDirWithinBase(target.mcpPath, filepath.Dir(target.mcpPath)); err != nil {
			return err
		}
		if err := mutations.SnapshotFile(target.mcpPath); err != nil {
			return err
		}
	}

	settingsAgentPath := toHomeTildePath(home, agentsDir)
	mcpBinaryName, _ := localArtifactBinaryNames(goos)
	mcpCommandPath := toHomeTildePath(home, filepath.Join(paths.binaryDir, mcpBinaryName))

	if err := ctx.Err(); err != nil {
		return err
	}

	settingsEdits := make([]state.SettingsEdit, 0, len(configTargets))
	mcpEdits := make([]state.MCPEdit, 0, len(configTargets))
	for _, target := range configTargets {
		settingsEdit, err := config.ApplySettingsEdit(target.settingsPath, settingsAgentPath, previousGlobal, stateFilePerm)
		if err != nil {
			return err
		}
		settingsEdits = append(settingsEdits, settingsEdit)

		mcpEdit, err := config.ApplyMCPEdit(target.mcpPath, mcpCommandPath, previousGlobal, stateFilePerm)
		if err != nil {
			return err
		}
		mcpEdits = append(mcpEdits, mcpEdit)

		r.reportDetail("updated settings: %s", target.settingsPath)
		r.reportDetail("updated mcp config: %s", target.mcpPath)
	}
	r.reportStepOK("Updated VS Code settings and MCP config", "")

	tracked := state.TrackedState{
		Version:     state.TrackedSchemaVersion,
		Repo:        release.Repo,
		ReleaseID:   rel.ID,
		ReleaseTag:  rel.TagName,
		InstalledAt: r.now().UTC().Format(time.RFC3339),
		Managed: state.ManagedState{
			Files: files.UniqueSorted(append(append([]string{}, binaryPaths...), extractedFiles...)),
			Dirs:  files.UniqueSorted(append(mutations.CreatedDirectories(), extractedDirs...)),
		},
		AppliedSteps: []state.AppliedStep{{
			ID:         "global.mutations",
			InputsHash: hashInputs(map[string]any{"command": commandNameForInstallOrUpdate(isUpdate), "release": rel.TagName, "targets": configTargets}),
			Outputs:    map[string]any{"binaryDir": paths.binaryDir, "agentsDir": agentsDir},
			AppliedAt:  r.now().UTC().Format(time.RFC3339),
		}},
		JSONEdits: state.TrackedJSONOpsFromEdits(settingsEdits, mcpEdits),
	}
	if previousState != nil {
		tracked.Local = append(tracked.Local, previousState.Local...)
	}

	if isUpdate {
		if previousGlobal == nil {
			r.reportDetail("no previously tracked files; skipped stale cleanup")
		} else {
			daemonPath := filepath.Join(paths.binaryDir, ccsubagentsdBinaryName(goos))
			if containsCleanPath(previousGlobal.Managed.Files, daemonPath) && !containsCleanPath(binaryPaths, daemonPath) {
				if err := mutations.SnapshotFile(daemonPath); err != nil {
					return err
				}
				if err := os.Remove(daemonPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("remove stale managed daemon binary %s: %w", daemonPath, err)
				}
			}

			if err := ctx.Err(); err != nil {
				return err
			}
			if err := files.RemoveStaleAgentFilesWithHook(previousGlobal.Managed.Files, extractedFiles, agentsDir, rollback.CaptureFile); err != nil {
				return err
			}
			r.reportStepOK("Removed stale managed agent files", "")
		}
	}

	if err := r.persistPendingPinWrite(mutations); err != nil {
		return err
	}

	if err := state.SaveTrackedState(stateDir, tracked); err != nil {
		return err
	}
	if err := txSession.Commit(); err != nil {
		return err
	}
	r.reportDetail("saved tracked state: %s", filepath.Join(stateDir, state.TrackedFileName))
	r.reportCompletion(commandNameForInstallOrUpdate(isUpdate))
	r.reportGlobalPathWarning(home)

	return nil
}

func (r *Runner) uninstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	home, err := r.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	layout := paths.Global(home)
	if stateOverride := strings.TrimSpace(getenv(paths.EnvStateDir)); stateOverride != "" {
		layout.StateDir = filepath.Clean(stateOverride)
	}
	paths := resolveInstallPaths(home)

	stateDir := layout.StateDir
	tracked, err := state.LoadTrackedState(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.reportStepOK("Checked tracked installation state", "none found")
			return nil
		}
		return err
	}

	if strings.TrimSpace(tracked.ReleaseTag) == "" {
		r.reportStepOK("Checked tracked installation state", "found")
	} else {
		r.reportStepOK("Checked tracked installation state", fmt.Sprintf("%s found", tracked.ReleaseTag))
	}
	if err := r.stopDaemonBeforeRemoval(ctx); err != nil {
		return err
	}

	agentsDir := filepath.Join(layout.StateDir, "agents")
	allowedConfigParentDirs := []string{
		filepath.Dir(paths.stable.settingsPath),
		filepath.Dir(paths.stable.mcpPath),
		filepath.Dir(paths.insiders.settingsPath),
		filepath.Dir(paths.insiders.mcpPath),
	}
	for _, edit := range tracked.JSONEdits.AllSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		allowedConfigParentDirs = append(allowedConfigParentDirs, filepath.Dir(edit.File))
	}
	for _, edit := range tracked.JSONEdits.AllMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		allowedConfigParentDirs = append(allowedConfigParentDirs, filepath.Dir(edit.File))
	}
	allowedConfigParentDirs = files.UniqueSorted(allowedConfigParentDirs)
	mcpBinaryName, webBinaryName := localArtifactBinaryNames(runtime.GOOS)
	allowedBinaries := []string{filepath.Join(paths.binaryDir, mcpBinaryName), filepath.Join(paths.binaryDir, webBinaryName), filepath.Join(paths.binaryDir, ccsubagentsdBinaryName(runtime.GOOS))}

	for _, path := range tracked.Managed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if !files.IsAllowedManagedPath(clean, agentsDir, allowedBinaries) {
			return fmt.Errorf("refusing to delete unsafe tracked path: %s", clean)
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("remove %s: %w (requires privileges for %s)", clean, err, paths.binaryDir)
			}
			return fmt.Errorf("remove %s: %w", clean, err)
		}
	}
	r.reportStepOK("Removed managed files", "")

	if err := ctx.Err(); err != nil {
		return err
	}
	for _, edit := range tracked.JSONEdits.AllSettingsEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := config.RevertSettingsEdit(edit, stateFilePerm); err != nil {
			return err
		}
	}
	for _, edit := range tracked.JSONEdits.AllMCPEdits() {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		if err := config.RevertMCPEdit(edit, stateFilePerm); err != nil {
			return err
		}
	}
	r.reportStepOK("Reverted settings and MCP configuration", "")

	dirs := append([]string{}, tracked.Managed.Dirs...)
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
	r.reportStepOK("Removed managed directories", "")

	if err := ctx.Err(); err != nil {
		return err
	}
	tracked.ClearGlobalInstall()
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
	r.reportCompletion("Uninstall")

	return nil
}

func hashInputs(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
