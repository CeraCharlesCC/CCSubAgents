package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveConfiguredPath expands configured home-relative values into absolute paths.
func ResolveConfiguredPath(home, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" {
		return filepath.Clean(home)
	}
	if strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "~\\") {
		remainder := strings.TrimLeft(trimmed[2:], `/\\`)
		if remainder == "" {
			return filepath.Clean(home)
		}
		return filepath.Join(home, remainder)
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if os.PathSeparator == '\\' && (strings.HasPrefix(trimmed, `\`) || strings.HasPrefix(trimmed, "/")) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(home, trimmed)
}
