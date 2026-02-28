package installer

import "path/filepath"

func containsCleanPath(paths []string, target string) bool {
	cleanTarget := filepath.Clean(target)
	for _, candidate := range paths {
		if filepath.Clean(candidate) == cleanTarget {
			return true
		}
	}
	return false
}
