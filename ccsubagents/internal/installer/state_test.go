package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

func TestSaveTrackedState_UsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not reliable on windows")
	}

	stateDir := t.TempDir()
	trackedPath := filepath.Join(stateDir, state.TrackedFileName)
	if err := os.WriteFile(trackedPath, []byte("{\"version\":1}\n"), stateFilePerm); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	tracked := state.TrackedState{
		Version:    state.TrackedSchemaVersion,
		Repo:       release.Repo,
		ReleaseTag: "v-test",
	}
	if err := state.SaveTrackedState(stateDir, tracked); err != nil {
		t.Fatalf("save tracked state: %v", err)
	}

	info, err := os.Stat(trackedPath)
	if err != nil {
		t.Fatalf("stat tracked state: %v", err)
	}

	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		t.Fatalf("expected tracked state to deny group/other access, got mode %#o", perm)
	}
}
