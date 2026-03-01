package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonctl"
)

func TestStopDaemonBeforeRemoval_IgnoresMetadataOnlyRegistryIssues(t *testing.T) {
	resetDaemonUninstallHooks(t)
	newDefaultDaemonClientFn = func(string, func(string) string) (*daemonclient.Client, error) {
		return daemonclient.NewUnavailableClient(errors.New("already unavailable")), nil
	}
	waitForDaemonStopFn = func(context.Context, string, time.Duration) error {
		return nil
	}
	stopRegisteredProcessesFn = func(context.Context, string, []string) error {
		return fmt.Errorf("%w: skip invalid pid filename /tmp/web/abc.pid: parse pid", daemonctl.ErrProcessRegistryMetadata)
	}

	var status bytes.Buffer
	runner := &Runner{
		homeDir:   func() (string, error) { return t.TempDir(), nil },
		statusOut: &status,
	}

	if err := runner.stopDaemonBeforeRemoval(context.Background()); err != nil {
		t.Fatalf("expected metadata-only registry issues to be ignored, got %v", err)
	}
	if !strings.Contains(status.String(), "Ignoring stale daemon process metadata") {
		t.Fatalf("expected warning output, got %q", status.String())
	}
}

func TestStopDaemonBeforeRemoval_FailsOnNonMetadataRegistryErrors(t *testing.T) {
	resetDaemonUninstallHooks(t)
	newDefaultDaemonClientFn = func(string, func(string) string) (*daemonclient.Client, error) {
		return daemonclient.NewUnavailableClient(errors.New("already unavailable")), nil
	}
	waitForDaemonStopFn = func(context.Context, string, time.Duration) error {
		return nil
	}
	stopRegisteredProcessesFn = func(context.Context, string, []string) error {
		return errors.New("failed to enumerate registered processes")
	}

	runner := &Runner{
		homeDir: func() (string, error) { return t.TempDir(), nil },
	}

	err := runner.stopDaemonBeforeRemoval(context.Background())
	if err == nil {
		t.Fatalf("expected non-metadata registry error")
	}
	if !strings.Contains(err.Error(), "stop registered daemon processes") {
		t.Fatalf("expected stop registered daemon processes error, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to enumerate registered processes") {
		t.Fatalf("expected wrapped registry failure, got %v", err)
	}
}

func resetDaemonUninstallHooks(t *testing.T) {
	t.Helper()
	origNewDefaultDaemonClientFn := newDefaultDaemonClientFn
	origWaitForDaemonStopFn := waitForDaemonStopFn
	origStopRegisteredProcessesFn := stopRegisteredProcessesFn
	t.Cleanup(func() {
		newDefaultDaemonClientFn = origNewDefaultDaemonClientFn
		waitForDaemonStopFn = origWaitForDaemonStopFn
		stopRegisteredProcessesFn = origStopRegisteredProcessesFn
	})
}
