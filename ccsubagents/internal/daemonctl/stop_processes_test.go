package daemonctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStopRegisteredProcesses_RemovesStalePIDFile(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedPIDFile(t, stateDir, "web", "4242.pid")

	processExistsFn = func(pid int) bool {
		return false
	}
	sendGracefulFn = func(pid int) error {
		t.Fatalf("sendGraceful should not be called for stale pid")
		return nil
	}
	sendForceFn = func(pid int) error {
		t.Fatalf("sendForce should not be called for stale pid")
		return nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"}, os.Stderr)
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected stale pid file to be removed, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_RemovesInvalidPIDFileNames(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	invalidA := seedPIDFile(t, stateDir, "web", "abc.pid")
	invalidB := seedPIDFile(t, stateDir, "web", "123.txt")

	processExistsFn = func(pid int) bool {
		return false
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"}, os.Stderr)
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if _, statErr := os.Stat(invalidA); !os.IsNotExist(statErr) {
		t.Fatalf("expected invalid abc.pid file to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(invalidB); !os.IsNotExist(statErr) {
		t.Fatalf("expected invalid 123.txt file to be removed, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_GracefulThenForce(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedPIDFile(t, stateDir, "web", "300.pid")

	var gracefulCalled, forceCalled bool
	forced := false
	processExistsFn = func(pid int) bool {
		if pid != 300 {
			return false
		}
		return !forced
	}
	sendGracefulFn = func(pid int) error {
		if pid != 300 {
			t.Fatalf("unexpected graceful pid=%d", pid)
		}
		gracefulCalled = true
		return nil
	}
	sendForceFn = func(pid int) error {
		if pid != 300 {
			t.Fatalf("unexpected force pid=%d", pid)
		}
		forceCalled = true
		forced = true
		return nil
	}

	clock := time.Unix(0, 0)
	stopNow = func() time.Time {
		return clock
	}
	stopSleep = func(d time.Duration) {
		clock = clock.Add(d)
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"}, os.Stderr)
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if !gracefulCalled {
		t.Fatalf("expected graceful stop to be attempted")
	}
	if !forceCalled {
		t.Fatalf("expected force stop to be attempted after graceful timeout")
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected pid file removed after stop, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_GracefulErrorFallsBackToForce(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedPIDFile(t, stateDir, "web", "301.pid")

	alive := true
	var gracefulCalled, forceCalled bool
	processExistsFn = func(pid int) bool {
		if pid != 301 {
			return false
		}
		return alive
	}
	sendGracefulFn = func(pid int) error {
		if pid != 301 {
			t.Fatalf("unexpected graceful pid=%d", pid)
		}
		gracefulCalled = true
		return errors.New("interrupt not supported")
	}
	sendForceFn = func(pid int) error {
		if pid != 301 {
			t.Fatalf("unexpected force pid=%d", pid)
		}
		forceCalled = true
		alive = false
		return nil
	}

	clock := time.Unix(0, 0)
	stopNow = func() time.Time {
		return clock
	}
	stopSleep = func(d time.Duration) {
		clock = clock.Add(d)
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"}, os.Stderr)
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if !gracefulCalled {
		t.Fatalf("expected graceful stop to be attempted")
	}
	if !forceCalled {
		t.Fatalf("expected force stop fallback after graceful error")
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected pid file removed after stop, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_AggregatesErrorsAndContinues(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	failingFile := seedPIDFile(t, stateDir, "web", "111.pid")
	successFile := seedPIDFile(t, stateDir, "web", "222.pid")

	alive222 := true
	processExistsFn = func(pid int) bool {
		switch pid {
		case 111:
			return true
		case 222:
			return alive222
		default:
			return false
		}
	}
	sendGracefulFn = func(pid int) error {
		switch pid {
		case 111:
			return errors.New("permission denied")
		case 222:
			alive222 = false
			return nil
		default:
			return nil
		}
	}
	sendForceFn = func(pid int) error {
		return nil
	}

	clock := time.Unix(0, 0)
	stopNow = func() time.Time {
		return clock
	}
	stopSleep = func(d time.Duration) {
		clock = clock.Add(d)
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"}, os.Stderr)
	if err == nil {
		t.Fatalf("expected aggregated error when one pid fails to stop")
	}
	if !strings.Contains(err.Error(), "pid 111") {
		t.Fatalf("expected error to mention failed pid 111, got %v", err)
	}
	if _, statErr := os.Stat(successFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected successful pid file removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(failingFile); statErr != nil {
		t.Fatalf("expected failing pid file to remain for retry, stat err=%v", statErr)
	}
}

func resetStopProcessHooks(t *testing.T) {
	t.Helper()
	origProcessExists := processExistsFn
	origSendGraceful := sendGracefulFn
	origSendForce := sendForceFn
	origStopSleep := stopSleep
	origStopNow := stopNow
	t.Cleanup(func() {
		processExistsFn = origProcessExists
		sendGracefulFn = origSendGraceful
		sendForceFn = origSendForce
		stopSleep = origStopSleep
		stopNow = origStopNow
	})
}

func seedPIDFile(t *testing.T, stateDir, role, name string) string {
	t.Helper()
	roleDir := registryRoleDir(stateDir, role)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		t.Fatalf("create role dir: %v", err)
	}
	path := filepath.Join(roleDir, name)
	if err := os.WriteFile(path, []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write pid file %s: %v", name, err)
	}
	return path
}
