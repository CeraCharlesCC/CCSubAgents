package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

const (
	LocalManagedDirRelativePath = ".ccsubagents"
	LocalAgentsRelativePath     = ".github/agents"
	LocalMCPRelativePath        = ".vscode/mcp.json"
	LocalMCPCommandBase         = "${workspaceFolder}/.ccsubagents/local-artifact-mcp"
)

func LocalMCPCommand(goos string) string {
	if strings.EqualFold(goos, "windows") {
		return LocalMCPCommandBase + ".exe"
	}
	return LocalMCPCommandBase
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

func ResolveLocalIgnoreTarget(installRoot, repoRoot string, mode state.LocalInstallMode) (string, []string, error) {
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
	case state.LocalInstallModePersonal:
		excludePath, err := resolveGitExcludePath(repoRoot)
		if err != nil {
			return "", nil, err
		}
		lines := []string{
			toRepoPath(LocalManagedDirRelativePath),
			toRepoPath(filepath.Join(LocalAgentsRelativePath, "*.agent.md")),
		}
		return excludePath, lines, nil
	case state.LocalInstallModeTeam:
		gitIgnorePath := filepath.Join(repoRoot, ".gitignore")
		lines := []string{toRepoPath(LocalManagedDirRelativePath)}
		return gitIgnorePath, lines, nil
	default:
		return "", nil, nil
	}
}

func ApplyLocalIgnoreRules(installRoot, repoRoot string, mode state.LocalInstallMode, dirPerm, filePerm os.FileMode) ([]state.IgnoreEdit, error) {
	path, lines, err := ResolveLocalIgnoreTarget(installRoot, repoRoot, mode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" || len(lines) == 0 {
		return nil, nil
	}

	added, err := appendMissingIgnoreLines(path, lines, dirPerm, filePerm)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return nil, nil
	}
	return []state.IgnoreEdit{{File: path, AddedLines: added}}, nil
}

func MergeIgnoreEdits(base []state.IgnoreEdit, next []state.IgnoreEdit) []state.IgnoreEdit {
	out := append([]state.IgnoreEdit{}, base...)
	for _, edit := range next {
		if strings.TrimSpace(edit.File) == "" {
			continue
		}
		merged := false
		for idx := range out {
			if filepath.Clean(out[idx].File) != filepath.Clean(edit.File) {
				continue
			}
			out[idx].AddedLines = files.UniqueSorted(append(out[idx].AddedLines, edit.AddedLines...))
			merged = true
			break
		}
		if !merged {
			copyEdit := state.IgnoreEdit{File: edit.File, AddedLines: files.UniqueSorted(edit.AddedLines)}
			out = append(out, copyEdit)
		}
	}
	return out
}

func appendMissingIgnoreLines(path string, lines []string, dirPerm, filePerm os.FileMode) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
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
	if err := files.WriteFileAtomic(path, []byte(builder), filePerm); err != nil {
		return nil, fmt.Errorf("write ignore file %s: %w", path, err)
	}
	return added, nil
}

func RevertIgnoreEdits(edits []state.IgnoreEdit, filePerm os.FileMode) error {
	for _, edit := range edits {
		if strings.TrimSpace(edit.File) == "" || len(edit.AddedLines) == 0 {
			continue
		}
		if err := removeIgnoreLines(edit.File, edit.AddedLines, filePerm); err != nil {
			return err
		}
	}
	return nil
}

func removeIgnoreLines(path string, lines []string, filePerm os.FileMode) error {
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
	if err := files.WriteFileAtomic(path, []byte(out), filePerm); err != nil {
		return fmt.Errorf("write ignore file %s during uninstall: %w", path, err)
	}
	return nil
}

func DetectExistingTeamLocalSetup(installRoot, repoRoot string) (bool, error) {
	ignorePath, lines, err := ResolveLocalIgnoreTarget(installRoot, repoRoot, state.LocalInstallModeTeam)
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

	agentsExist, err := managedAgentFilesExist(filepath.Join(installRoot, LocalAgentsRelativePath))
	if err != nil {
		return false, err
	}
	if !agentsExist {
		return false, nil
	}

	mcpHasLocalCommand, err := localMCPCommandConfigured(filepath.Join(installRoot, LocalMCPRelativePath))
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

// localMCPCommandConfigured validates the configured command for the current
// runtime OS only; shared repositories may need per-platform mcp.json values.
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
	serverRaw, ok := servers[MCPServerKey]
	if !ok {
		return false, nil
	}
	server, ok := serverRaw.(map[string]any)
	if !ok {
		return false, nil
	}
	command, _ := server["command"].(string)
	return strings.TrimSpace(command) == LocalMCPCommand(runtime.GOOS), nil
}
