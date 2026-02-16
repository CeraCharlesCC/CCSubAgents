package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

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

func isDirNotEmptyError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ENOTEMPTY)
}
