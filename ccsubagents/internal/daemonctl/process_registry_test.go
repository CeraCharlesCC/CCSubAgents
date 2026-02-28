package daemonctl

import (
	"path/filepath"
	"testing"
)

func TestRegistryRoleDir_UnsafeRoleFallsBackToBase(t *testing.T) {
	stateDir := t.TempDir()
	base := filepath.Join(stateDir, "daemon", "processes")

	got := registryRoleDir(stateDir, "../../../../etc")
	if got != base {
		t.Fatalf("registryRoleDir() = %q, want %q", got, base)
	}
}

func TestSanitizeRegistryRole(t *testing.T) {
	tests := []struct {
		name string
		role string
		ok   bool
	}{
		{name: "empty", role: "", ok: false},
		{name: "safe", role: "web", ok: true},
		{name: "safe trimmed", role: "  mcp  ", ok: true},
		{name: "dotdot", role: "..", ok: false},
		{name: "traversal", role: "../web", ok: false},
		{name: "slash", role: "web/ui", ok: false},
		{name: "backslash", role: `web\ui`, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := sanitizeRegistryRole(tc.role)
			if ok != tc.ok {
				t.Fatalf("sanitizeRegistryRole(%q) ok=%v, want %v", tc.role, ok, tc.ok)
			}
		})
	}
}
