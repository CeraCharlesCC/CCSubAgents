package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRegisterProcessPID_CreatesPIDFileAndUnregisters(t *testing.T) {
	stateDir := t.TempDir()
	const (
		role = "mcp"
		pid  = 4242
	)

	unregister, err := RegisterProcessPID(stateDir, role, pid)
	if err != nil {
		t.Fatalf("register pid: %v", err)
	}

	pidPath := filepath.Join(ProcessRegistryRoleDir(stateDir, role), "4242.pid")
	contents, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if string(contents) != "4242\n" {
		t.Fatalf("pid file contents mismatch: got=%q want=%q", string(contents), "4242\\n")
	}

	if runtime.GOOS != "windows" {
		roleInfo, err := os.Stat(ProcessRegistryRoleDir(stateDir, role))
		if err != nil {
			t.Fatalf("stat role dir: %v", err)
		}
		if mode := roleInfo.Mode().Perm(); mode != 0o755 {
			t.Fatalf("role dir mode mismatch: got=%#o want=%#o", mode, 0o755)
		}

		pidInfo, err := os.Stat(pidPath)
		if err != nil {
			t.Fatalf("stat pid file: %v", err)
		}
		if mode := pidInfo.Mode().Perm(); mode != 0o644 {
			t.Fatalf("pid file mode mismatch: got=%#o want=%#o", mode, 0o644)
		}
	}

	if err := unregister(); err != nil {
		t.Fatalf("unregister pid: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected pid file removed, stat err=%v", err)
	}
}

func TestRegisterProcessPID_IdempotentRewriteForSamePID(t *testing.T) {
	stateDir := t.TempDir()
	const (
		role = "mcp"
		pid  = 4242
	)

	if _, err := RegisterProcessPID(stateDir, role, pid); err != nil {
		t.Fatalf("first register pid: %v", err)
	}
	unregister, err := RegisterProcessPID(stateDir, role, pid)
	if err != nil {
		t.Fatalf("second register pid: %v", err)
	}

	pidPath := filepath.Join(ProcessRegistryRoleDir(stateDir, role), "4242.pid")
	contents, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file after rewrite: %v", err)
	}
	if string(contents) != "4242\n" {
		t.Fatalf("pid file contents mismatch after rewrite: got=%q want=%q", string(contents), "4242\\n")
	}

	if err := unregister(); err != nil {
		t.Fatalf("unregister pid: %v", err)
	}
}

func TestUnregisterProcessPID_IdempotentWhenFileMissing(t *testing.T) {
	stateDir := t.TempDir()
	if err := UnregisterProcessPID(stateDir, "web", 5050); err != nil {
		t.Fatalf("unregister missing pid file: %v", err)
	}
}
