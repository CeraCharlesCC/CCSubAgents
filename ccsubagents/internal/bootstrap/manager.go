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
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

var installBinaryFunc = installBinary

type Manager struct {
	httpClient *http.Client
	now        func() time.Time
	homeDir    func() (string, error)
	lookPath   func(string) (string, error)
	runCommand func(context.Context, string, ...string) ([]byte, error)
}

func NewManager() *Manager {
	return &Manager{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		now:        time.Now,
		homeDir:    os.UserHomeDir,
		lookPath:   exec.LookPath,
		runCommand: runCommand,
	}
}

type installPaths struct {
	binaryDir    string
	settingsPath string
	mcpPath      string
}

func resolveInstallPaths(home string) installPaths {
	paths := installPaths{
		binaryDir:    filepath.Join(home, binaryInstallDirDefaultRel),
		settingsPath: filepath.Join(home, settingsRelativePath),
		mcpPath:      filepath.Join(home, mcpConfigRelativePath),
	}

	if override := resolveConfiguredPath(home, os.Getenv(binaryInstallDirEnv)); override != "" {
		paths.binaryDir = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(settingsPathEnv)); override != "" {
		paths.settingsPath = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(mcpConfigPathEnv)); override != "" {
		paths.mcpPath = override
	}

	return paths
}

func resolveConfiguredPath(home, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" {
		return filepath.Clean(home)
	}
	if strings.HasPrefix(trimmed, "~"+string(os.PathSeparator)) {
		return filepath.Join(home, trimmed[2:])
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(home, trimmed)
}

func toHomeTildePath(home, path string) string {
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(home)
	if cleanHome == "" || cleanHome == "." {
		return filepath.ToSlash(cleanPath)
	}

	rel, err := filepath.Rel(cleanHome, cleanPath)
	if err == nil {
		rel = filepath.Clean(rel)
		if rel == "." {
			return "~"
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}

	return filepath.ToSlash(cleanPath)
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return out, nil
}

func (m *Manager) Run(ctx context.Context, command string) error {
	switch strings.TrimSpace(command) {
	case installCommand:
		return m.installOrUpdate(ctx, false)
	case updateCommand:
		return m.installOrUpdate(ctx, true)
	case uninstallCommand:
		return m.uninstall(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}

type releaseResponse struct {
	ID      int64          `json:"id"`
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type trackedState struct {
	Version     int            `json:"version"`
	Repo        string         `json:"repo"`
	ReleaseID   int64          `json:"releaseId"`
	ReleaseTag  string         `json:"releaseTag"`
	InstalledAt string         `json:"installedAt"`
	Managed     managedState   `json:"managed"`
	JSONEdits   trackedJSONOps `json:"jsonEdits"`
}

type managedState struct {
	Files []string `json:"files"`
	Dirs  []string `json:"dirs"`
}

type trackedJSONOps struct {
	Settings settingsEdit `json:"settings"`
	MCP      mcpEdit      `json:"mcp"`
}

type settingsEdit struct {
	File      string `json:"file"`
	AgentPath string `json:"agentPath"`
	Mode      string `json:"mode,omitempty"`
	Added     bool   `json:"added"`
}

type mcpEdit struct {
	File        string          `json:"file"`
	Key         string          `json:"key"`
	Touched     bool            `json:"touched"`
	HadPrevious bool            `json:"hadPrevious"`
	Previous    json.RawMessage `json:"previous,omitempty"`
}

type fileSnapshot struct {
	exists bool
	mode   os.FileMode
	data   []byte
}

type installRollback struct {
	snapshots   map[string]fileSnapshot
	createdDirs []string
}

func newInstallRollback() *installRollback {
	return &installRollback{snapshots: map[string]fileSnapshot{}}
}

func (r *installRollback) captureFile(path string) error {
	clean := filepath.Clean(path)
	if _, ok := r.snapshots[clean]; ok {
		return nil
	}
	info, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.snapshots[clean] = fileSnapshot{exists: false}
			return nil
		}
		return fmt.Errorf("stat %s for rollback: %w", clean, err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot snapshot directory for rollback: %s", clean)
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return fmt.Errorf("read %s for rollback: %w", clean, err)
	}
	r.snapshots[clean] = fileSnapshot{
		exists: true,
		mode:   info.Mode().Perm(),
		data:   data,
	}
	return nil
}

func (r *installRollback) trackCreatedDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.createdDirs = append(r.createdDirs, filepath.Clean(path))
}

func (r *installRollback) restore() error {
	paths := make([]string, 0, len(r.snapshots))
	for path := range r.snapshots {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var errs []string
	for _, path := range paths {
		snapshot := r.snapshots[path]
		if snapshot.exists {
			if err := os.WriteFile(path, snapshot.data, snapshot.mode); err != nil {
				errs = append(errs, fmt.Sprintf("restore %s: %v", path, err))
			}
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Sprintf("remove %s: %v", path, err))
		}
	}

	dirs := uniqueSorted(r.createdDirs)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if strings.Contains(strings.ToLower(err.Error()), "directory not empty") {
				continue
			}
			errs = append(errs, fmt.Sprintf("remove created dir %s: %v", dir, err))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) installOrUpdate(ctx context.Context, isUpdate bool) (retErr error) {
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

	for _, binaryName := range []string{assetArtifactMCP, assetArtifactWeb} {
		src := bundleBinaries[binaryName]
		dst := filepath.Join(paths.binaryDir, binaryName)
		if err := rollback.captureFile(dst); err != nil {
			return err
		}
		if err := installBinaryFunc(src, dst); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("install %s into %s: %w (requires privileges to write %s)", binaryName, paths.binaryDir, err, paths.binaryDir)
			}
			return fmt.Errorf("install %s into %s: %w", binaryName, paths.binaryDir, err)
		}
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
	// VS Code/Copilot config paths are user-scoped and should be stored as "~/".
	// Absolute paths can be rejected or warned on by clients that validate path format.
	settingsAgentPath := toHomeTildePath(home, agentsDir)
	mcpCommandPath := toHomeTildePath(home, filepath.Join(paths.binaryDir, assetArtifactMCP))

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
	_ = ctx
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
		clean := filepath.Clean(dir)
		if !isAllowedManagedDirectory(clean, agentsDir, allowedConfigParentDirs) {
			return fmt.Errorf("refusing to delete unsafe tracked directory: %s", clean)
		}
		if err := os.Remove(clean); err != nil {
			if errors.Is(err, os.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "directory not empty") {
				continue
			}
			return fmt.Errorf("remove tracked directory %s: %w", clean, err)
		}
	}

	if err := os.Remove(filepath.Join(stateDir, trackedFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove tracked state: %w", err)
	}

	return nil
}

func uniqueSorted(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func mapRequiredAssets(assets []releaseAsset, names []string) (map[string]releaseAsset, error) {
	byName := make(map[string]releaseAsset, len(assets))
	for _, asset := range assets {
		byName[asset.Name] = asset
	}
	out := make(map[string]releaseAsset, len(names))
	for _, name := range names {
		asset, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("latest release is missing required asset %q", name)
		}
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			return nil, fmt.Errorf("release asset %q has no download URL", name)
		}
		out[name] = asset
	}
	return out, nil
}

func (m *Manager) fetchLatestRelease(ctx context.Context) (releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseLatestURL, nil)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("create latest release request: %w", err)
	}
	req.Header.Set("Accept", httpsHeaderAccept)
	req.Header.Set("User-Agent", httpsHeaderUserAgent)
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(httpsHeaderAuthorization, httpsHeaderGithubTokenPref+token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("request latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return releaseResponse{}, fmt.Errorf("latest release request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return releaseResponse{}, fmt.Errorf("decode latest release response: %w", err)
	}
	if strings.TrimSpace(decoded.TagName) == "" {
		return releaseResponse{}, errors.New("latest release response is missing tag_name")
	}
	return decoded, nil
}

func (m *Manager) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", httpsHeaderUserAgent)
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(httpsHeaderAuthorization, httpsHeaderGithubTokenPref+token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stateFilePerm)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("copy response to destination: %w", err)
	}
	return nil
}

func (m *Manager) verifyDownloadedAssets(ctx context.Context, downloaded map[string]string) error {
	if _, err := m.lookPath("gh"); err != nil {
		return errors.New("gh CLI is required for attestation verification but was not found in PATH")
	}

	names := make([]string, 0, len(downloaded))
	for name := range downloaded {
		names = append(names, name)
	}
	sort.Strings(names)

	certIdentity := "https://github.com/" + releaseRepo + "/" + releaseWorkflowPath + "@refs/heads/main"
	for _, name := range names {
		path := downloaded[name]
		_, err := m.runCommand(
			ctx,
			"gh",
			"attestation",
			"verify",
			path,
			"--repo",
			releaseRepo,
			"--cert-identity",
			certIdentity,
			"--cert-oidc-issuer",
			attestationOIDCIssuer,
		)
		if err != nil {
			return fmt.Errorf("attestation verification failed for %s: %w", name, err)
		}
	}
	return nil
}

func ensureDirTracked(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("path exists but is not a directory: %s", path)
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(path, stateDirPerm); err != nil {
		return false, fmt.Errorf("create directory %s: %w", path, err)
	}
	return true, nil
}

func installBinary(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source binary %s: %w", srcPath, err)
	}
	if err := os.WriteFile(dstPath, data, binaryFilePerm); err != nil {
		return err
	}
	return nil
}

func extractBundleBinaries(zipPath, destDir string, names []string) (map[string]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer r.Close()

	expected := map[string]struct{}{}
	for _, name := range names {
		expected[name] = struct{}{}
	}

	extracted := map[string]string{}
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name := strings.TrimSpace(file.Name)
		if name == "" {
			continue
		}
		clean := filepath.Clean(name)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			return nil, fmt.Errorf("unsafe archive path: %s", file.Name)
		}

		baseName := filepath.Base(clean)
		if _, ok := expected[baseName]; !ok {
			continue
		}
		if _, exists := extracted[baseName]; exists {
			return nil, fmt.Errorf("archive contains duplicate %q", baseName)
		}

		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open archive file %s: %w", file.Name, err)
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read archive file %s: %w", file.Name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close archive file %s: %w", file.Name, closeErr)
		}

		destPath := filepath.Join(destDir, baseName)
		if err := os.WriteFile(destPath, content, binaryFilePerm); err != nil {
			return nil, fmt.Errorf("write extracted bundle file %s: %w", destPath, err)
		}
		extracted[baseName] = destPath
	}

	missing := []string{}
	for _, name := range names {
		if _, ok := extracted[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("archive missing required file(s): %s", strings.Join(missing, ", "))
	}

	return extracted, nil
}

func extractAgentsArchiveWithHook(zipPath, destDir string, beforeWrite func(string) error) (filesOut []string, dirsOut []string, retErr error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open archive: %w", err)
	}
	defer r.Close()

	stripAgentsPrefix, err := shouldStripAgentsPrefix(r.File)
	if err != nil {
		return nil, nil, err
	}

	files := []string{}
	dirs := []string{}
	writtenFiles := []string{}
	defer func() {
		if retErr == nil {
			return
		}
		for _, filePath := range writtenFiles {
			_ = os.Remove(filePath)
		}
	}()

	for _, file := range r.File {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			continue
		}
		clean := filepath.Clean(name)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			return nil, nil, fmt.Errorf("unsafe archive path: %s", file.Name)
		}

		if stripAgentsPrefix {
			if clean == "agents" {
				continue
			}
			clean = strings.TrimPrefix(clean, "agents/")
			if strings.TrimSpace(clean) == "" || clean == "." {
				continue
			}
		}

		destPath := filepath.Join(destDir, clean)
		destPath = filepath.Clean(destPath)
		if !isPathWithinDir(destPath, destDir) {
			return nil, nil, fmt.Errorf("archive path escapes destination: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, stateDirPerm); err != nil {
				return nil, nil, fmt.Errorf("create directory %s: %w", destPath, err)
			}
			dirs = append(dirs, destPath)
			continue
		}

		parent := filepath.Dir(destPath)
		if err := os.MkdirAll(parent, stateDirPerm); err != nil {
			return nil, nil, fmt.Errorf("create directory %s: %w", parent, err)
		}
		dirs = append(dirs, parent)

		rc, err := file.Open()
		if err != nil {
			return nil, nil, fmt.Errorf("open archive file %s: %w", file.Name, err)
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read archive file %s: %w", file.Name, readErr)
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close archive file %s: %w", file.Name, closeErr)
		}
		mode := file.FileInfo().Mode().Perm()
		if mode == 0 {
			mode = stateFilePerm
		}
		if beforeWrite != nil {
			if err := beforeWrite(destPath); err != nil {
				return nil, nil, err
			}
		}
		if err := os.WriteFile(destPath, content, mode); err != nil {
			return nil, nil, fmt.Errorf("write extracted file %s: %w", destPath, err)
		}
		writtenFiles = append(writtenFiles, destPath)
		files = append(files, destPath)
	}

	return uniqueSorted(files), uniqueSorted(dirs), nil
}

func shouldStripAgentsPrefix(files []*zip.File) (bool, error) {
	seen := false
	for _, file := range files {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			continue
		}
		clean := filepath.Clean(name)
		if clean == "." {
			continue
		}
		if clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			return false, fmt.Errorf("unsafe archive path: %s", file.Name)
		}
		seen = true
		if clean == "agents" || strings.HasPrefix(clean, "agents/") {
			continue
		}
		return false, nil
	}
	return seen, nil
}

func removeStaleAgentFilesWithHook(oldFiles, newFiles []string, agentsDir string, beforeRemove func(string) error) error {
	newSet := map[string]struct{}{}
	for _, path := range newFiles {
		newSet[filepath.Clean(path)] = struct{}{}
	}

	for _, path := range oldFiles {
		clean := filepath.Clean(path)
		if !isPathWithinDir(clean, agentsDir) {
			continue
		}
		if _, keep := newSet[clean]; keep {
			continue
		}
		if beforeRemove != nil {
			if err := beforeRemove(clean); err != nil {
				return err
			}
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale managed agent file %s: %w", clean, err)
		}
	}
	return nil
}

func applySettingsEdit(settingsPath, agentsDir string, previous *trackedState) (settingsEdit, error) {
	root, err := readJSONFile(settingsPath)
	if err != nil {
		return settingsEdit{}, fmt.Errorf("read settings.json: %w", err)
	}

	added := false
	if current, exists := root[settingsAgentPathKey]; exists {
		locations, ok := current.(map[string]any)
		if !ok {
			return settingsEdit{}, fmt.Errorf("settings %s must be an object when present", settingsAgentPathKey)
		}
		if previous != nil {
			previousPath := strings.TrimSpace(previous.JSONEdits.Settings.AgentPath)
			if previousPath != "" && previousPath != agentsDir {
				delete(locations, previousPath)
			}
		}
		if _, exists := locations[agentsDir]; !exists {
			locations[agentsDir] = true
			added = true
		}
	} else {
		root[settingsAgentPathKey] = map[string]any{agentsDir: true}
		added = true
	}

	if chatRaw, chatExists := root["chat"]; chatExists {
		if _, ok := chatRaw.(map[string]any); !ok {
			return settingsEdit{}, errors.New("settings key chat must be an object when present")
		}
	}

	if err := writeJSONFile(settingsPath, root); err != nil {
		return settingsEdit{}, fmt.Errorf("write settings.json: %w", err)
	}

	wasAdded := added
	if previous != nil && previous.JSONEdits.Settings.Added {
		wasAdded = true
	}
	return settingsEdit{File: settingsPath, AgentPath: agentsDir, Added: wasAdded}, nil
}

func revertSettingsEdit(edit settingsEdit) error {
	if !edit.Added {
		return nil
	}
	root, err := readJSONFile(edit.File)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read settings.json for uninstall: %w", err)
	}

	locationsRaw, ok := root[settingsAgentPathKey]
	if !ok {
		return nil
	}
	locations, ok := locationsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("settings %s must be an object when present", settingsAgentPathKey)
	}
	if _, exists := locations[edit.AgentPath]; !exists {
		return nil
	}
	delete(locations, edit.AgentPath)

	if err := writeJSONFile(edit.File, root); err != nil {
		return fmt.Errorf("write settings.json during uninstall: %w", err)
	}
	return nil
}

func applyMCPEdit(path, commandPath string, previous *trackedState) (mcpEdit, error) {
	root, err := readJSONFile(path)
	if err != nil {
		return mcpEdit{}, fmt.Errorf("read mcp.json: %w", err)
	}

	servers, err := ensureObject(root, "servers")
	if err != nil {
		return mcpEdit{}, fmt.Errorf("mcp key servers: %w", err)
	}

	edit := mcpEdit{File: path, Key: mcpServerKey, Touched: true}
	if prev := previous; prev != nil && prev.JSONEdits.MCP.Touched {
		edit.HadPrevious = prev.JSONEdits.MCP.HadPrevious
		if len(prev.JSONEdits.MCP.Previous) > 0 {
			edit.Previous = slices.Clone(prev.JSONEdits.MCP.Previous)
		}
	} else if existing, ok := servers[mcpServerKey]; ok {
		encoded, err := json.Marshal(existing)
		if err != nil {
			return mcpEdit{}, fmt.Errorf("marshal existing mcp server config: %w", err)
		}
		edit.HadPrevious = true
		edit.Previous = encoded
	}

	servers[mcpServerKey] = map[string]any{
		"command": commandPath,
	}

	if err := writeJSONFile(path, root); err != nil {
		return mcpEdit{}, fmt.Errorf("write mcp.json: %w", err)
	}

	return edit, nil
}

func revertMCPEdit(edit mcpEdit) error {
	if !edit.Touched {
		return nil
	}
	root, err := readJSONFile(edit.File)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read mcp.json for uninstall: %w", err)
	}

	servers, err := ensureObject(root, "servers")
	if err != nil {
		return fmt.Errorf("mcp key servers: %w", err)
	}

	if edit.HadPrevious {
		if len(edit.Previous) == 0 {
			return errors.New("tracked mcp previous value is missing")
		}
		var restored any
		if err := json.Unmarshal(edit.Previous, &restored); err != nil {
			return fmt.Errorf("decode tracked previous mcp value: %w", err)
		}
		servers[edit.Key] = restored
	} else {
		delete(servers, edit.Key)
	}

	if err := writeJSONFile(edit.File, root); err != nil {
		return fmt.Errorf("write mcp.json during uninstall: %w", err)
	}
	return nil
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

func ensureObject(root map[string]any, key string) (map[string]any, error) {
	v, exists := root[key]
	if !exists {
		next := map[string]any{}
		root[key] = next
		return next, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object when present", key)
	}
	return obj, nil
}

func ensureParentDir(path string) (bool, error) {
	parent := filepath.Dir(path)
	return ensureDirTracked(parent)
}

func isPathWithinDir(path, dir string) bool {
	pathClean := filepath.Clean(path)
	dirClean := filepath.Clean(dir)
	if pathClean == dirClean {
		return true
	}
	return strings.HasPrefix(pathClean, dirClean+string(os.PathSeparator))
}

func isAllowedManagedPath(path, agentsDir string, allowedBinaries []string) bool {
	for _, binaryPath := range allowedBinaries {
		if path == filepath.Clean(binaryPath) {
			return true
		}
	}
	return isPathWithinDir(path, agentsDir)
}

func isAllowedManagedDirectory(path, agentsDir string, allowedConfigParentDirs []string) bool {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(filepath.Dir(agentsDir)) {
		return true
	}
	if isPathWithinDir(clean, agentsDir) {
		return true
	}
	for _, configParentDir := range allowedConfigParentDirs {
		if clean == filepath.Clean(configParentDir) {
			return true
		}
	}
	return false
}

func (m *Manager) trackedStatePath(stateDir string) string {
	return filepath.Join(stateDir, trackedFileName)
}

func (m *Manager) loadTrackedState(stateDir string) (*trackedState, error) {
	trackedPath := m.trackedStatePath(stateDir)
	b, err := os.ReadFile(trackedPath)
	if err != nil {
		return nil, fmt.Errorf("read tracked state %s: %w", trackedPath, err)
	}
	var state trackedState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("parse tracked state %s: %w", trackedPath, err)
	}
	if state.Version == 0 {
		return nil, fmt.Errorf("tracked state %s is missing version", trackedPath)
	}
	return &state, nil
}

func (m *Manager) loadTrackedStateForInstall(stateDir string) (*trackedState, error) {
	state, err := m.loadTrackedState(stateDir)
	if err == nil {
		return state, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("tracked state is unreadable; resolve %s and retry: %w", m.trackedStatePath(stateDir), err)
}

func (m *Manager) saveTrackedState(stateDir string, state trackedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tracked state: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(stateDir, ".tracked-*.json")
	if err != nil {
		return fmt.Errorf("create tracked temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tracked temp file: %w", err)
	}
	if err := tmp.Chmod(stateFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod tracked temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tracked temp file: %w", err)
	}

	if err := os.Rename(tmpPath, m.trackedStatePath(stateDir)); err != nil {
		return fmt.Errorf("replace tracked state: %w", err)
	}
	return nil
}
