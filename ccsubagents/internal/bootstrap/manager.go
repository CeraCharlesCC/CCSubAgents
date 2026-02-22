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
	localManagedDirRelativePath   = ".ccsubagents"
	localAgentsRelativePath       = ".github/agents"
	localMCPRelativePath          = ".vscode/mcp.json"
	localMCPCommand               = "${workspaceFolder}/.ccsubagents/local-artifact-mcp"
	trackedSchemaVersion          = 2
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

func parseCommaSeparatedChoices(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func makeCustomConfigTarget(base string) installConfigTarget {
	cleanBase := filepath.Clean(base)
	return installConfigTarget{
		settingsPath: filepath.Join(cleanBase, "data", "Machine", "settings.json"),
		mcpPath:      filepath.Join(cleanBase, "data", "User", "mcp.json"),
	}
}

func trimConfigFileSuffix(path string, suffixParts ...string) (string, bool) {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	suffix := filepath.Clean(filepath.Join(suffixParts...))
	if !strings.HasSuffix(cleanPath, suffix) {
		return "", false
	}

	base := strings.TrimSuffix(cleanPath, suffix)
	for strings.HasSuffix(base, string(os.PathSeparator)) {
		base = strings.TrimSuffix(base, string(os.PathSeparator))
	}
	if strings.TrimSpace(base) == "" {
		base = string(os.PathSeparator)
	}

	return filepath.Clean(base), true
}

func describeGlobalInstallTargetPath(home string, target installConfigTarget) string {
	settingsPath := filepath.Clean(strings.TrimSpace(target.settingsPath))
	mcpPath := filepath.Clean(strings.TrimSpace(target.mcpPath))

	settingsRoot, settingsHasRoot := trimConfigFileSuffix(settingsPath, "data", "Machine", "settings.json")
	mcpRoot, mcpHasRoot := trimConfigFileSuffix(mcpPath, "data", "User", "mcp.json")
	if settingsHasRoot && mcpHasRoot && filepath.Clean(settingsRoot) == filepath.Clean(mcpRoot) {
		return toHomeTildePath(home, settingsRoot)
	}

	settingsDisplay := toHomeTildePath(home, settingsPath)
	mcpDisplay := toHomeTildePath(home, mcpPath)
	if settingsDisplay == mcpDisplay {
		return settingsDisplay
	}

	return fmt.Sprintf("settings: %s; mcp: %s", settingsDisplay, mcpDisplay)
}

func (m *Manager) promptGlobalInstallTargets(ctx context.Context, home string, paths installPaths) ([]installConfigTarget, error) {
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
			return nil, err
		}

		insidersDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.insiders.settingsPath,
			mcpPath:      paths.insiders.mcpPath,
		})
		stableDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.stable.settingsPath,
			mcpPath:      paths.stable.mcpPath,
		})

if _, err := io.WriteString(output, "Where should ccsubagents be installed?\n\n"); err != nil {
return nil, fmt.Errorf("write install target prompt: %w", err)
}
		if _, err := fmt.Fprintf(output, "[1] VS Code Server — Insiders   (%s)\n", insidersDisplay); err != nil {
			return nil, fmt.Errorf("write install target prompt: %w", err)
		}
		if _, err := fmt.Fprintf(output, "[2] VS Code Server — Stable     (%s)\n", stableDisplay); err != nil {
			return nil, fmt.Errorf("write install target prompt: %w", err)
		}
if _, err := io.WriteString(output, "[3] Custom path(s)\n"); err != nil {
return nil, fmt.Errorf("write install target prompt: %w", err)
}
if _, err := io.WriteString(output, "\n"); err != nil {
return nil, fmt.Errorf("write install target prompt: %w", err)
}
if _, err := io.WriteString(output, "Choice (comma-separated, e.g. 1,2): "); err != nil {
return nil, fmt.Errorf("write install target prompt: %w", err)
}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read install target selection: %w", err)
		}

		choices := parseCommaSeparatedChoices(line)
		selected := map[string]struct{}{}
		valid := true
		for _, choice := range choices {
			switch choice {
			case "1", "2", "3":
				selected[choice] = struct{}{}
			default:
				valid = false
			}
		}

		if len(selected) == 0 || !valid {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("global install target selection canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter comma-separated values using 1, 2, and/or 3.\n\n"); writeErr != nil {
				return nil, fmt.Errorf("write install target prompt: %w", writeErr)
			}
			continue
		}

		targets := make([]installConfigTarget, 0, len(selected)+1)
		if _, ok := selected["1"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath})
		}
		if _, ok := selected["2"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.stable.settingsPath, mcpPath: paths.stable.mcpPath})
		}
		if _, ok := selected["3"]; ok {
			if _, err := io.WriteString(output, "Enter custom target path(s), comma-separated: "); err != nil {
				return nil, fmt.Errorf("write custom target prompt: %w", err)
			}
			customLine, customErr := reader.ReadString('\n')
			if customErr != nil && !errors.Is(customErr, io.EOF) {
				return nil, fmt.Errorf("read custom target paths: %w", customErr)
			}

			customPaths := parseCommaSeparatedChoices(customLine)
			if len(customPaths) == 0 {
				if errors.Is(customErr, io.EOF) {
					return nil, errors.New("custom install target selection canceled")
				}
				if _, writeErr := io.WriteString(output, "Custom paths cannot be empty.\n\n"); writeErr != nil {
					return nil, fmt.Errorf("write custom target prompt: %w", writeErr)
				}
				continue
			}

			for _, customPath := range customPaths {
				resolved := resolveConfiguredPath(home, customPath)
				if strings.TrimSpace(resolved) == "" {
					continue
				}
				targets = append(targets, makeCustomConfigTarget(resolved))
			}
		}

		targets = uniqueInstallTargets(targets)
		if len(targets) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("global install target selection canceled")
			}
			if _, writeErr := io.WriteString(output, "No valid targets selected.\n\n"); writeErr != nil {
				return nil, fmt.Errorf("write install target prompt: %w", writeErr)
			}
			continue
		}

		return targets, nil
	}
}

func (m *Manager) Run(ctx context.Context, command Command, scope Scope) error {
	switch scope {
	case ScopeGlobal:
		switch command {
		case CommandInstall:
			home, err := m.homeDir()
			if err != nil {
				return fmt.Errorf("determine home directory: %w", err)
			}
			paths := resolveInstallPaths(home)
			targets, err := m.promptGlobalInstallTargets(ctx, home, paths)
			if err != nil {
				return err
			}
			m.globalInstallTargets = targets
			return m.installOrUpdate(ctx, false)
		case CommandUpdate:
			m.globalInstallTargets = nil
			return m.installOrUpdate(ctx, true)
		case CommandUninstall:
			m.globalInstallTargets = nil
			return m.uninstall(ctx)
		default:
			return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
		}
	case ScopeLocal:
		switch command {
		case CommandInstall:
			return m.installLocal(ctx)
		case CommandUpdate:
			return m.updateLocal(ctx)
		case CommandUninstall:
			return m.uninstallLocal(ctx)
		default:
			return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
		}
	default:
		return fmt.Errorf("unknown scope %q (expected: local, global)", scope)
	}
}

func (m *Manager) reportVersionHeader(tag string) {
	m.statusf("ccsubagents %s\n", tag)
}

func (m *Manager) reportStepOK(summary, trailing string) {
	if strings.TrimSpace(trailing) == "" {
		m.statusf("✓ %s\n", summary)
		return
	}
	m.statusf("✓ %s (%s)\n", summary, trailing)
}

func (m *Manager) reportStepFail(summary string) {
	m.statusf("✗ %s\n", summary)
}

func (m *Manager) reportDetail(format string, args ...any) {
	if !m.verbose {
		return
	}
	m.statusf("  %s\n", fmt.Sprintf(format, args...))
}

func (m *Manager) reportMessageLine(format string, args ...any) {
	m.statusf("  %s\n", fmt.Sprintf(format, args...))
}

func (m *Manager) reportWarning(headline string, details ...string) {
m.statusf("\n⚠ %s\n", headline)
for _, detail := range details {
if strings.TrimSpace(detail) == "" {
continue
		}
		m.statusf("  %s\n", detail)
	}
}

func (m *Manager) reportCompletion(command string) {
	m.statusf("%s complete.\n", command)
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
	previousGlobal := previousState.globalInstallSnapshot()

	var configTargets []installConfigTarget
	if isUpdate {
		configTargets = resolveUpdateTargets(paths, previousGlobal)
	} else {
		if len(m.globalInstallTargets) > 0 {
			configTargets = uniqueInstallTargets(m.globalInstallTargets)
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
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	if isUpdate && previousGlobal != nil && release.TagName == previousGlobal.ReleaseTag {
		m.statusf("ccsubagents: already at latest version (%s). Nothing to do.\n", release.TagName)
		return nil
	}

	m.reportVersionHeader(release.TagName)
	if previousGlobal == nil {
		m.reportStepOK("Checked for existing installation", "none found")
	} else {
		m.reportStepOK("Checked for existing installation", fmt.Sprintf("%s found", previousGlobal.ReleaseTag))
	}
	m.reportDetail("tracked state path: %s", filepath.Join(stateDir, trackedFileName))

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
		if info, statErr := os.Stat(dest); statErr == nil {
			m.reportDetail("downloaded %s (%d bytes)", name, info.Size())
		}
	}
	m.reportStepOK("Downloaded release assets", release.TagName)

	if m.skipAttestationsCheck {
		m.reportStepOK("Verified attestations", "skipped (--skip-attestations-check)")
		m.reportDetail("attestation verification skipped by flag")
	} else {
		if err := m.verifyDownloadedAssets(ctx, downloaded); err != nil {
			var attestationErr *attestationVerificationError
			if errors.As(err, &attestationErr) {
				m.reportStepFail("Verified attestations")
				m.reportMessageLine("Failed asset: %s", attestationErr.Asset)
				return fmt.Errorf("Error: attestation verification failed for %s\nTo skip verification: ccsubagents install --skip-attestations-check\n(not recommended for production use)", attestationErr.Asset)
			}
			return err
		}
		m.reportStepOK("Verified attestations", "")
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
	m.reportDetail("extracted bundle %s", assetLocalArtifactZip)

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
		m.reportDetail("installed binary: %s", binaryName)
	}
	m.reportStepOK("Installed binaries", fmt.Sprintf("→ %s", toHomeTildePath(home, paths.binaryDir)))

	if err := ctx.Err(); err != nil {
		return err
	}

	extractedFiles, extractedDirs, err := extractAgentsArchiveWithHook(downloaded[assetAgentsZip], agentsDir, rollback.captureFile)
	if err != nil {
		return fmt.Errorf("extract %s into %s: %w", assetAgentsZip, agentsDir, err)
	}
	m.reportStepOK("Installed agent definitions", fmt.Sprintf("→ %s", toHomeTildePath(home, agentsDir)))
	m.reportDetail("extracted %d agent definitions", len(extractedFiles))

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

	settingsEdits := make([]settingsEdit, 0, len(configTargets))
	mcpEdits := make([]mcpEdit, 0, len(configTargets))
	for _, target := range configTargets {
		settingsEdit, err := applySettingsEdit(target.settingsPath, settingsAgentPath, previousGlobal)
		if err != nil {
			return err
		}
		settingsEdits = append(settingsEdits, settingsEdit)

		mcpEdit, err := applyMCPEdit(target.mcpPath, mcpCommandPath, previousGlobal)
		if err != nil {
			return err
		}
		mcpEdits = append(mcpEdits, mcpEdit)

		m.reportDetail("updated settings: %s", target.settingsPath)
		m.reportDetail("updated mcp config: %s", target.mcpPath)
	}
	m.reportStepOK("Updated VS Code settings and MCP config", "")

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
	if previousState != nil {
		state.Local = append(state.Local, previousState.Local...)
	}

	if isUpdate {
		if previousGlobal == nil {
			m.reportDetail("no previously tracked files; skipped stale cleanup")
		} else {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := removeStaleAgentFilesWithHook(previousGlobal.Managed.Files, extractedFiles, agentsDir, rollback.captureFile); err != nil {
				return err
			}
			m.reportStepOK("Removed stale managed agent files", "")
		}
	}

	if err := m.saveTrackedState(stateDir, state); err != nil {
		return err
	}
	m.reportDetail("saved tracked state: %s", filepath.Join(stateDir, trackedFileName))
	m.reportCompletion(commandNameForInstallOrUpdate(isUpdate))
	m.reportGlobalPathWarning(home)

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
			m.reportStepOK("Checked tracked installation state", "none found")
			return nil
		}
		return err
	}

	if strings.TrimSpace(state.ReleaseTag) == "" {
		m.reportStepOK("Checked tracked installation state", "found")
	} else {
		m.reportStepOK("Checked tracked installation state", fmt.Sprintf("%s found", state.ReleaseTag))
	}

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
	m.reportStepOK("Removed managed files", "")

	if err := ctx.Err(); err != nil {
		return err
	}
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
	m.reportStepOK("Reverted settings and MCP configuration", "")

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
	m.reportStepOK("Removed managed directories", "")

	if err := ctx.Err(); err != nil {
		return err
	}
	state.clearGlobalInstall()
	state.Version = trackedSchemaVersion
	if state.empty() {
		if err := os.Remove(filepath.Join(stateDir, trackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove tracked state: %w", err)
		}
		m.reportStepOK("Updated tracked installation state", "removed")
	} else {
		if err := m.saveTrackedState(stateDir, *state); err != nil {
			return err
		}
		m.reportStepOK("Updated tracked installation state", "saved")
	}
	m.reportCompletion("Uninstall")

	return nil
}
