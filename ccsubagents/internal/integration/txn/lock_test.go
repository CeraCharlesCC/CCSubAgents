package txn

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBegin_RecoversStaleLockFromDeadPID(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	blobDir := filepath.Join(t.TempDir(), "blob")
	if err := os.MkdirAll(filepath.Join(stateDir, "locks"), 0o755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	lockPath := filepath.Join(stateDir, "locks", "global.lock")
	payload := []byte(`{"pid":99999999,"createdAt":"2026-01-01T00:00:00Z"}`)
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	session, err := Begin(stateDir, blobDir, "global", "install", []string{"step"})
	if err != nil {
		t.Fatalf("expected stale lock recovery, got %v", err)
	}
	session.Close()
}

func TestBegin_DoesNotBreakActiveLock(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	blobDir := filepath.Join(t.TempDir(), "blob")
	if err := os.MkdirAll(filepath.Join(stateDir, "locks"), 0o755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	lockPath := filepath.Join(stateDir, "locks", "global.lock")
	payload := []byte(`{"pid":` + fmt.Sprintf("%d", os.Getpid()) + `,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`)
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatalf("write active lock: %v", err)
	}

	if _, err := Begin(stateDir, blobDir, "global", "install", []string{"step"}); err == nil {
		t.Fatal("expected active lock to block begin")
	}
}

func TestRecoverStaleLock_WindowsFallbackRecoversOldPIDLock(t *testing.T) {
	originalGOOS := runtimeGOOS
	runtimeGOOS = "windows"
	t.Cleanup(func() { runtimeGOOS = originalGOOS })

	lockPath := filepath.Join(t.TempDir(), "global.lock")
	createdAt := time.Now().Add(-staleLockMaxAge - time.Hour).UTC()
	payload := []byte(`{"pid":` + fmt.Sprintf("%d", os.Getpid()) + `,"createdAt":"` + createdAt.Format(time.RFC3339) + `"}`)
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	recovered, err := recoverStaleLock(lockPath, time.Now().UTC())
	if err != nil {
		t.Fatalf("recoverStaleLock returned error: %v", err)
	}
	if !recovered {
		t.Fatal("expected old windows pid lock to be recovered")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected recovered lock file removed, stat err=%v", err)
	}
}

func TestShouldTreatPIDLockAsStale(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-staleLockMaxAge - time.Second)
	recent := now.Add(-time.Hour)

	if !shouldTreatPIDLockAsStale("windows", old, now) {
		t.Fatal("expected windows old pid lock to be treated as stale")
	}
	if shouldTreatPIDLockAsStale("windows", recent, now) {
		t.Fatal("expected recent windows pid lock to remain active")
	}
	if shouldTreatPIDLockAsStale("linux", old, now) {
		t.Fatal("expected non-windows pid lock fallback to stay disabled")
	}
}
