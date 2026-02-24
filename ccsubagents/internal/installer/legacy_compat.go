package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	cfg "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	f "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	rls "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	st "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

type Manager = Runner

func NewManager() *Manager {
	return NewRunner()
}

type releaseResponse = rls.Response
type releaseAsset = rls.Asset
type releaseNotFoundError = rls.ReleaseNotFoundError
type attestationVerificationError = rls.AttestationVerificationError

var errReleaseNotFound = rls.ErrReleaseNotFound

const (
	releaseLatestURL     = rls.LatestURL
	releaseTagsURLPrefix = rls.TagsURLPrefix
	httpsHeaderAccept    = rls.HeaderAccept
	httpsHeaderUserAgent = rls.HeaderUserAgent

	releaseRepo          = rls.Repo
	trackedFileName      = st.TrackedFileName
	trackedSchemaVersion = st.TrackedSchemaVersion

	mcpServerKey                = cfg.MCPServerKey
	settingsAgentPathKey        = cfg.SettingsAgentPathKey
	localManagedDirRelativePath = cfg.LocalManagedDirRelativePath
	localAgentsRelativePath     = cfg.LocalAgentsRelativePath
	localMCPRelativePath        = cfg.LocalMCPRelativePath
	localMCPCommand             = cfg.LocalMCPCommand
	agentsRelativePath          = ".local/share/ccsubagents/agents"
	settingsRelativePath        = settingsInsidersRelativePath
	mcpConfigRelativePath       = mcpConfigInsidersRelativePath
)

type trackedState = st.TrackedState
type localInstall = st.LocalInstall
type localInstallMode = st.LocalInstallMode
type ignoreEdit = st.IgnoreEdit
type managedState = st.ManagedState
type trackedJSONOps = st.TrackedJSONOps
type settingsEdit = st.SettingsEdit
type mcpEdit = st.MCPEdit

type installSettings = cfg.InstallSettings
type settingsScope = cfg.SettingsScope

const (
	localInstallModePersonal = st.LocalInstallModePersonal
	localInstallModeTeam     = st.LocalInstallModeTeam
	settingsScopeGlobal      = cfg.SettingsScopeGlobal
	settingsScopeLocal       = cfg.SettingsScopeLocal
)

func trackedJSONOpsFromEdits(settings []settingsEdit, mcp []mcpEdit) trackedJSONOps {
	return st.TrackedJSONOpsFromEdits(settings, mcp)
}

func normalizeVersionTag(raw string) string {
	return cfg.NormalizeVersionTag(raw)
}

func loadMergedInstallSettings(home, cwd string) (installSettings, error) {
	return cfg.LoadMergedInstallSettings(home, cwd)
}

func resolveSettingsPaths(home, cwd string) (string, string) {
	return cfg.ResolveSettingsPaths(home, cwd)
}

func choosePinWritePath(cwd, home string) (string, settingsScope, error) {
	return cfg.ChoosePinWritePath(cwd, home)
}

func writePinnedVersion(path, versionTag string) error {
	return cfg.WritePinnedVersion(path, versionTag, stateDirPerm, stateFilePerm)
}

func applyLocalIgnoreRules(installRoot, repoRoot string, mode localInstallMode) ([]ignoreEdit, error) {
	return cfg.ApplyLocalIgnoreRules(installRoot, repoRoot, mode, stateDirPerm, stateFilePerm)
}

func mapRequiredAssets(assets []releaseAsset, names []string) (map[string]releaseAsset, error) {
	return rls.MapRequiredAssets(assets, names)
}

func (r *Runner) fetchLatestRelease(ctx context.Context) (releaseResponse, error) {
	return r.releaseClient().FetchLatest(ctx)
}

func (r *Runner) fetchReleaseByTag(ctx context.Context, tag string) (releaseResponse, error) {
	return r.releaseClient().FetchByTag(ctx, tag)
}

func (r *Runner) downloadFile(ctx context.Context, url, destPath string) error {
	return r.releaseClient().DownloadFile(ctx, url, destPath, stateFilePerm)
}

func (r *Runner) verifyDownloadedAssets(ctx context.Context, downloaded map[string]string) error {
	return r.releaseClient().VerifyDownloadedAssets(ctx, downloaded, r.reportDetail)
}

func (r *Runner) trackedStatePath(stateDir string) string {
	return st.TrackedStatePath(stateDir)
}

func (r *Runner) loadTrackedState(stateDir string) (*trackedState, error) {
	return st.LoadTrackedState(stateDir)
}

func (r *Runner) loadTrackedStateForInstall(stateDir string) (*trackedState, error) {
	return st.LoadTrackedStateForInstall(stateDir)
}

func (r *Runner) saveTrackedState(stateDir string, state trackedState) error {
	return st.SaveTrackedState(stateDir, state)
}

func readJSONFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func writeJSONFile(path string, v map[string]any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, stateFilePerm)
}

func applySettingsEdit(settingsPath, agentsDir string, previous *trackedState) (settingsEdit, error) {
	return cfg.ApplySettingsEdit(settingsPath, agentsDir, previous, stateFilePerm)
}

func revertSettingsEdit(edit settingsEdit) error {
	return cfg.RevertSettingsEdit(edit, stateFilePerm)
}

func applyMCPEdit(path, commandPath string, previous *trackedState) (mcpEdit, error) {
	return cfg.ApplyMCPEdit(path, commandPath, previous, stateFilePerm)
}

func revertMCPEdit(edit mcpEdit) error {
	return cfg.RevertMCPEdit(edit, stateFilePerm)
}

func isAllowedManagedPath(path, agentsDir string, allowedBinaries []string) bool {
	return f.IsAllowedManagedPath(path, agentsDir, allowedBinaries)
}

func extractAgentsArchiveWithHook(zipPath, destDir string, beforeWrite func(string) error) (filesOut []string, dirsOut []string, retErr error) {
	return f.ExtractAgentsArchiveWithHook(zipPath, destDir, beforeWrite, stateDirPerm, stateFilePerm)
}

type installRollback struct {
	inner       *f.Rollback
	createdDirs []string
}

func newInstallRollback() *installRollback {
	return &installRollback{inner: f.NewRollback()}
}

func (r *installRollback) captureFile(path string) error {
	return r.inner.CaptureFile(path)
}

func (r *installRollback) trackCreatedDir(path string) {
	r.createdDirs = append(r.createdDirs, path)
	r.inner.TrackCreatedDir(path)
}

func (r *installRollback) restore() error {
	return r.inner.Restore()
}

type mutationTracker struct {
	rollback    *installRollback
	createdDirs []string
}

func newMutationTracker(rollback *installRollback) *mutationTracker {
	return &mutationTracker{rollback: rollback}
}

func (m *mutationTracker) ensureDir(path string) error {
	created, err := f.EnsureDirTracked(path, stateDirPerm)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	clean := filepath.Clean(path)
	m.createdDirs = append(m.createdDirs, clean)
	m.rollback.trackCreatedDir(clean)
	return nil
}

func (m *mutationTracker) ensureParentDir(path string) error {
	return m.ensureDir(filepath.Dir(path))
}

func (m *mutationTracker) snapshotFile(path string) error {
	return m.rollback.captureFile(path)
}

func (m *mutationTracker) SnapshotFile(path string) error {
	return m.snapshotFile(path)
}

func (m *mutationTracker) createdDirectories() []string {
	return f.UniqueSorted(m.createdDirs)
}
