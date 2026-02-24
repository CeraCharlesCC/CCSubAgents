package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMutationTracker_EnsureDir_TracksCreatedDirectory(t *testing.T) {
	rollback := newInstallRollback()
	tracker := newMutationTracker(rollback)

	createdDir := filepath.Join(t.TempDir(), "created")
	if err := tracker.ensureDir(createdDir); err != nil {
		t.Fatalf("ensureDir should create directory: %v", err)
	}
	if err := tracker.ensureDir(createdDir); err != nil {
		t.Fatalf("ensureDir should be idempotent for existing directory: %v", err)
	}

	if _, err := os.Stat(createdDir); err != nil {
		t.Fatalf("expected created directory to exist: %v", err)
	}

	clean := filepath.Clean(createdDir)
	if len(tracker.createdDirs) != 1 || tracker.createdDirs[0] != clean {
		t.Fatalf("expected created dir tracked once, got %#v", tracker.createdDirs)
	}
	if len(rollback.createdDirs) != 1 || rollback.createdDirs[0] != clean {
		t.Fatalf("expected rollback tracker to include created dir once, got %#v", rollback.createdDirs)
	}
}

func TestMutationTracker_EnsureParentDir_CreatesAndTracksParent(t *testing.T) {
	rollback := newInstallRollback()
	tracker := newMutationTracker(rollback)

	targetPath := filepath.Join(t.TempDir(), "nested", "config", "settings.json")
	if err := tracker.ensureParentDir(targetPath); err != nil {
		t.Fatalf("ensureParentDir should create parent directory: %v", err)
	}

	parentDir := filepath.Clean(filepath.Dir(targetPath))
	if _, err := os.Stat(parentDir); err != nil {
		t.Fatalf("expected parent directory to exist: %v", err)
	}
	if len(tracker.createdDirs) != 1 || tracker.createdDirs[0] != parentDir {
		t.Fatalf("expected created parent dir tracked once, got %#v", tracker.createdDirs)
	}
	if len(rollback.createdDirs) != 1 || rollback.createdDirs[0] != parentDir {
		t.Fatalf("expected rollback tracker to include parent dir once, got %#v", rollback.createdDirs)
	}
}

func TestMutationTracker_CreatedDirectories_ReturnsUniqueSorted(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "alpha")
	b := filepath.Join(base, "beta")
	c := filepath.Join(base, "gamma")

	tracker := &mutationTracker{
		rollback:    newInstallRollback(),
		createdDirs: []string{c, a, b, a, c},
	}

	got := tracker.createdDirectories()
	want := []string{a, b, c}
	if len(got) != len(want) {
		t.Fatalf("expected %d created directories, got %d (%#v)", len(want), len(got), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("expected sorted unique directories %#v, got %#v", want, got)
		}
	}
}
