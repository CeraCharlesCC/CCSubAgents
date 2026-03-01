package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStopRegisteredProcesses_RemovesStalePIDFile(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 4242, "start-4242")

	processExistsFn = func(pid int) bool {
		return false
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		t.Fatalf("processIdentityMatches should not be called for stale pid")
		return false, nil
	}
	sendGracefulFn = func(pid int) error {
		t.Fatalf("sendGraceful should not be called for stale pid")
		return nil
	}
	sendForceFn = func(pid int) error {
		t.Fatalf("sendForce should not be called for stale pid")
		return nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected stale pid file to be removed, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_ReportsInvalidPIDFileNamesWithoutDeleting(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	invalidA := seedPIDFileRaw(t, stateDir, "web", "abc.pid", []byte("invalid\n"))
	invalidB := seedPIDFileRaw(t, stateDir, "web", "123.txt", []byte("invalid\n"))

	processExistsFn = func(pid int) bool {
		t.Fatalf("processExists should not be called for invalid pid filenames")
		return true
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		t.Fatalf("processIdentityMatches should not be called for invalid pid filenames")
		return true, nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err == nil {
		t.Fatalf("expected error for invalid pid filename")
	}
	if !IsOnlyProcessRegistryMetadataIssues(err) {
		t.Fatalf("expected metadata-only issue classification, got %v", err)
	}
	if !strings.Contains(err.Error(), "abc.pid") {
		t.Fatalf("expected invalid pid filename to be reported, got %v", err)
	}
	if _, statErr := os.Stat(invalidA); statErr != nil {
		t.Fatalf("expected invalid abc.pid file to be preserved, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(invalidB); statErr != nil {
		t.Fatalf("expected non-pid file to be ignored, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_ReportsInvalidPIDFilePayloadWithoutDeleting(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	invalid := seedPIDFileRaw(t, stateDir, "web", "707.pid", []byte("707\n"))

	processExistsFn = func(pid int) bool {
		t.Fatalf("processExists should not be called for invalid pid payload")
		return true
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		t.Fatalf("processIdentityMatches should not be called for invalid pid payload")
		return true, nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err == nil {
		t.Fatalf("expected error for invalid pid payload")
	}
	if !IsOnlyProcessRegistryMetadataIssues(err) {
		t.Fatalf("expected metadata-only issue classification, got %v", err)
	}
	if !strings.Contains(err.Error(), "707.pid") {
		t.Fatalf("expected invalid pid payload to be reported, got %v", err)
	}
	if _, statErr := os.Stat(invalid); statErr != nil {
		t.Fatalf("expected invalid pid payload file to be preserved, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_RemovesMismatchedIdentityWithoutSignals(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 302, "expected-start")

	processExistsFn = func(pid int) bool {
		return pid == 302
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		if pid != 302 {
			t.Fatalf("unexpected identity check pid=%d", pid)
		}
		if startID != "expected-start" {
			t.Fatalf("unexpected start id=%q", startID)
		}
		return false, nil
	}
	sendGracefulFn = func(pid int) error {
		t.Fatalf("sendGraceful should not be called when identity mismatches")
		return nil
	}
	sendForceFn = func(pid int) error {
		t.Fatalf("sendForce should not be called when identity mismatches")
		return nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err != nil {
		t.Fatalf("StopRegisteredProcesses returned error: %v", err)
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected mismatched pid file to be removed, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_IdentityLookupErrorKeepsPIDFile(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 303, "expected-start")

	processExistsFn = func(pid int) bool {
		return pid == 303
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		if pid != 303 {
			t.Fatalf("unexpected identity check pid=%d", pid)
		}
		if startID != "expected-start" {
			t.Fatalf("unexpected start id=%q", startID)
		}
		return false, errors.New("permission denied")
	}
	sendGracefulFn = func(pid int) error {
		t.Fatalf("sendGraceful should not be called when identity lookup fails")
		return nil
	}
	sendForceFn = func(pid int) error {
		t.Fatalf("sendForce should not be called when identity lookup fails")
		return nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err == nil {
		t.Fatalf("expected identity lookup error")
	}
	if IsOnlyProcessRegistryMetadataIssues(err) {
		t.Fatalf("expected non-metadata classification for identity lookup failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "resolve web pid 303 identity") {
		t.Fatalf("expected identity lookup failure in error, got %v", err)
	}
	if _, statErr := os.Stat(pidFile); statErr != nil {
		t.Fatalf("expected pid file preserved for retry, stat err=%v", statErr)
	}
}

func TestStopRegisteredProcesses_GracefulThenForce(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 300, "start-300")

	var gracefulCalled, forceCalled bool
	forced := false
	processExistsFn = func(pid int) bool {
		if pid != 300 {
			return false
		}
		return !forced
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		return pid == 300 && startID == "start-300", nil
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

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
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
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 301, "start-301")

	alive := true
	var gracefulCalled, forceCalled bool
	processExistsFn = func(pid int) bool {
		if pid != 301 {
			return false
		}
		return alive
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		return pid == 301 && startID == "start-301", nil
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

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
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
	failingFile := seedRegisteredPIDFile(t, stateDir, "web", 111, "start-111")
	successFile := seedRegisteredPIDFile(t, stateDir, "web", 222, "start-222")

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
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		switch pid {
		case 111:
			return startID == "start-111", nil
		case 222:
			return startID == "start-222", nil
		default:
			return false, nil
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

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"web"})
	if err == nil {
		t.Fatalf("expected aggregated error when one pid fails to stop")
	}
	if IsOnlyProcessRegistryMetadataIssues(err) {
		t.Fatalf("expected non-metadata classification for stop failure, got %v", err)
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

func TestStopRegisteredProcesses_RejectsUnsafeRoleAndContinues(t *testing.T) {
	resetStopProcessHooks(t)
	stateDir := t.TempDir()
	pidFile := seedRegisteredPIDFile(t, stateDir, "web", 4242, "start-4242")

	processExistsFn = func(pid int) bool {
		return false
	}
	processIdentityMatchesFn = func(pid int, startID string) (bool, error) {
		t.Fatalf("processIdentityMatches should not be called for stale pid")
		return false, nil
	}
	sendGracefulFn = func(pid int) error {
		t.Fatalf("sendGraceful should not be called for stale pid")
		return nil
	}
	sendForceFn = func(pid int) error {
		t.Fatalf("sendForce should not be called for stale pid")
		return nil
	}

	err := StopRegisteredProcesses(context.Background(), stateDir, []string{"../../etc", "web"})
	if err == nil {
		t.Fatalf("expected error for unsafe role")
	}
	if !strings.Contains(err.Error(), `invalid role "../../etc"`) {
		t.Fatalf("expected invalid role error, got %v", err)
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected safe role pid file to still be processed, stat err=%v", statErr)
	}
}

func resetStopProcessHooks(t *testing.T) {
	t.Helper()
	origProcessExists := processExistsFn
	origProcessIdentityMatches := processIdentityMatchesFn
	origSendGraceful := sendGracefulFn
	origSendForce := sendForceFn
	origStopSleep := stopSleep
	origStopNow := stopNow
	t.Cleanup(func() {
		processExistsFn = origProcessExists
		processIdentityMatchesFn = origProcessIdentityMatches
		sendGracefulFn = origSendGraceful
		sendForceFn = origSendForce
		stopSleep = origStopSleep
		stopNow = origStopNow
	})
}

func seedRegisteredPIDFile(t *testing.T, stateDir, role string, pid int, startID string) string {
	t.Helper()
	payload, err := json.Marshal(pidFileRecord{
		PID:     pid,
		StartID: startID,
	})
	if err != nil {
		t.Fatalf("marshal pid file record: %v", err)
	}
	return seedPIDFileRaw(t, stateDir, role, fmt.Sprintf("%d%s", pid, processPIDFileSuffix), append(payload, '\n'))
}

func seedPIDFileRaw(t *testing.T, stateDir, role, name string, contents []byte) string {
	t.Helper()
	roleDir := registryRoleDir(stateDir, role)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		t.Fatalf("create role dir: %v", err)
	}
	path := filepath.Join(roleDir, name)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write pid file %s: %v", name, err)
	}
	return path
}
