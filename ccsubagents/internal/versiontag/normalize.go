package versiontag

import "strings"

// Normalize converts a user-provided version tag to canonical form.
func Normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "none") {
		return ""
	}
	if strings.EqualFold(trimmed, "null") {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "V") {
		return "v" + strings.TrimPrefix(trimmed, "V")
	}
	return "v" + trimmed
}
