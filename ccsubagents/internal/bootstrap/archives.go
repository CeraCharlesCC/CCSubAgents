package bootstrap

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
