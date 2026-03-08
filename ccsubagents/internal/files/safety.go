package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func UniqueSorted(values []string) []string {
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

func EnsureDirTracked(path string, perm os.FileMode) (bool, error) {
	return EnsureDirTrackedWithinBase(path, path, perm)
}

func EnsureDirTrackedWithinBase(path, base string, perm os.FileMode) (bool, error) {
	if err := RejectSymlinkPathWithinBase(path, base); err != nil {
		return false, err
	}

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
	if err := os.MkdirAll(path, perm); err != nil {
		return false, fmt.Errorf("create directory %s: %w", path, err)
	}
	return true, nil
}

func EnsureParentDir(path string, perm os.FileMode) (bool, error) {
	parent := filepath.Dir(path)
	return EnsureDirTracked(parent, perm)
}

func RejectSymlinkPath(path string) error {
	return RejectSymlinkPathWithinBase(path, path)
}

func RejectSymlinkPathWithinBase(path, base string) error {
	path = filepath.Clean(path)
	base = filepath.Clean(base)

	if path != base && !IsPathWithinDir(path, base) {
		return fmt.Errorf("path escapes trusted base: %s", path)
	}

	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing symlink path component: %s", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("lstat %s: %w", current, err)
		}

		if current == base {
			return nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("path %s does not descend from base %s", path, base)
		}
	}
}

func IsPathWithinDir(path, dir string) bool {
	pathClean := filepath.Clean(path)
	dirClean := filepath.Clean(dir)
	if pathClean == dirClean {
		return true
	}
	return strings.HasPrefix(pathClean, dirClean+string(os.PathSeparator))
}

func IsAllowedManagedPath(path, agentsDir string, allowedBinaries []string) bool {
	for _, binaryPath := range allowedBinaries {
		if path == filepath.Clean(binaryPath) {
			return true
		}
	}
	return IsPathWithinDir(path, agentsDir)
}

func IsAllowedManagedDirectory(path, agentsDir string, allowedConfigParentDirs []string) bool {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(filepath.Dir(agentsDir)) {
		return true
	}
	if IsPathWithinDir(clean, agentsDir) {
		return true
	}
	for _, configParentDir := range allowedConfigParentDirs {
		if clean == filepath.Clean(configParentDir) {
			return true
		}
	}
	return false
}
