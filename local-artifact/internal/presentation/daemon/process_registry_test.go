package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRegisterProcessPID_CreatesPIDFileAndUnregisters(t *testing.T) {
	stateDir := t.TempDir()
	const (
		role = "mcp"
	)
	pid := os.Getpid()

	unregister, err := RegisterProcessPID(stateDir, role, pid)
	if err != nil {
		t.Fatalf("register pid: %v", err)
	}

	pidPath := filepath.Join(ProcessRegistryRoleDir(stateDir, role), pidFileName(pid))
	contents, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}

	var record processPIDRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		t.Fatalf("unmarshal pid file json: %v", err)
	}
	if record.PID != pid {
		t.Fatalf("pid file PID mismatch: got=%d want=%d", record.PID, pid)
	}
	if strings.TrimSpace(record.StartID) == "" {
		t.Fatalf("pid file StartID must not be empty")
	}

	if runtime.GOOS != "windows" {
		roleInfo, err := os.Stat(ProcessRegistryRoleDir(stateDir, role))
		if err != nil {
			t.Fatalf("stat role dir: %v", err)
		}
		if mode := roleInfo.Mode().Perm(); mode&0o022 != 0 {
			t.Fatalf("role dir must not be group/world-writable: got=%#o", mode)
		}
		if mode := roleInfo.Mode().Perm(); mode&0o200 == 0 {
			t.Fatalf("role dir must be owner-writable: got=%#o", mode)
		}

		pidInfo, err := os.Stat(pidPath)
		if err != nil {
			t.Fatalf("stat pid file: %v", err)
		}
		if mode := pidInfo.Mode().Perm(); mode&0o022 != 0 {
			t.Fatalf("pid file must not be group/world-writable: got=%#o", mode)
		}
		if mode := pidInfo.Mode().Perm(); mode&0o200 == 0 {
			t.Fatalf("pid file must be owner-writable: got=%#o", mode)
		}
		if mode := pidInfo.Mode().Perm(); mode&0o111 != 0 {
			t.Fatalf("pid file must not be executable: got=%#o", mode)
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
	)
	pid := os.Getpid()

	if _, err := RegisterProcessPID(stateDir, role, pid); err != nil {
		t.Fatalf("first register pid: %v", err)
	}
	unregister, err := RegisterProcessPID(stateDir, role, pid)
	if err != nil {
		t.Fatalf("second register pid: %v", err)
	}

	pidPath := filepath.Join(ProcessRegistryRoleDir(stateDir, role), pidFileName(pid))
	contents, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file after rewrite: %v", err)
	}

	var record processPIDRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		t.Fatalf("unmarshal pid file json after rewrite: %v", err)
	}
	if record.PID != pid {
		t.Fatalf("pid file PID mismatch after rewrite: got=%d want=%d", record.PID, pid)
	}
	if strings.TrimSpace(record.StartID) == "" {
		t.Fatalf("pid file StartID must not be empty after rewrite")
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

func TestProcessRegistryRoleDir_UnsafeRoleFallsBackToBase(t *testing.T) {
	stateDir := t.TempDir()
	base := filepath.Join(stateDir, "daemon", processRegistryRootDir)

	got := ProcessRegistryRoleDir(stateDir, "../../../../etc")
	if got != base {
		t.Fatalf("ProcessRegistryRoleDir() = %q, want %q", got, base)
	}
}

func TestRegisterProcessPID_RejectsUnsafeRole(t *testing.T) {
	stateDir := t.TempDir()
	_, err := RegisterProcessPID(stateDir, "../mcp", os.Getpid())
	if err == nil {
		t.Fatalf("expected unsafe role to be rejected")
	}
}

func TestUnregisterProcessPID_RejectsUnsafeRole(t *testing.T) {
	stateDir := t.TempDir()
	err := UnregisterProcessPID(stateDir, "../web", 5050)
	if err == nil {
		t.Fatalf("expected unsafe role to be rejected")
	}
}
