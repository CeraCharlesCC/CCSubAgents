package mcp

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const workspaceHashOverrideEnv = "LOCAL_ARTIFACT_MCP_OVERRIDE_WORKSPACE_HASH"

func resolveWorkspaceHashOverrideFromEnv(getenv func(string) string) (string, error) {
	if getenv == nil {
		return "", nil
	}
	raw := strings.TrimSpace(getenv(workspaceHashOverrideEnv))
	if raw == "" {
		return "", nil
	}

	normalized := strings.ToLower(raw)
	if len(normalized) != 64 {
		return "", fmt.Errorf("%s must be 64 hex characters", workspaceHashOverrideEnv)
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", fmt.Errorf("%s must be hex: %w", workspaceHashOverrideEnv, err)
	}
	return normalized, nil
}
