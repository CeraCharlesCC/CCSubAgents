package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func TestLocalArtifactWebPath(t *testing.T) {
	base := filepath.Join("tmp", "bin")

	tests := []struct {
		name    string
		goos    string
		exePath string
		want    string
	}{
		{
			name:    "linux",
			goos:    "linux",
			exePath: filepath.Join(base, "local-artifact-mcp"),
			want:    filepath.Join(base, "local-artifact-web"),
		},
		{
			name:    "darwin",
			goos:    "darwin",
			exePath: filepath.Join(base, "local-artifact-mcp"),
			want:    filepath.Join(base, "local-artifact-web"),
		},
		{
			name:    "windows",
			goos:    "windows",
			exePath: filepath.Join(base, "local-artifact-mcp.exe"),
			want:    filepath.Join(base, "local-artifact-web.exe"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := localArtifactWebPath(tc.exePath, tc.goos)
			if got != tc.want {
				t.Fatalf("localArtifactWebPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCCSubagentsdPath(t *testing.T) {
	t.Run("prefers sibling binary", func(t *testing.T) {
		base := t.TempDir()
		exePath := filepath.Join(base, "local-artifact-mcp")
		sibling := filepath.Join(base, "ccsubagentsd")
		if err := os.WriteFile(sibling, []byte("daemon"), 0o755); err != nil {
			t.Fatalf("seed sibling daemon: %v", err)
		}

		got := ccsubagentsdPath(exePath, t.TempDir(), "linux",
			func(string) string { return "" },
			func(string) (string, error) { return "/usr/local/bin/ccsubagentsd", nil },
		)
		if got != sibling {
			t.Fatalf("ccsubagentsdPath() = %q, want %q", got, sibling)
		}
	})

	t.Run("uses configured bin dir when sibling missing", func(t *testing.T) {
		home := t.TempDir()
		configuredBin := filepath.Join(home, "bin")
		if err := os.MkdirAll(configuredBin, 0o755); err != nil {
			t.Fatalf("create configured bin dir: %v", err)
		}
		configured := filepath.Join(configuredBin, "ccsubagentsd")
		if err := os.WriteFile(configured, []byte("daemon"), 0o755); err != nil {
			t.Fatalf("seed configured daemon: %v", err)
		}

		got := ccsubagentsdPath(filepath.Join(t.TempDir(), "local-artifact-mcp"), home, "linux",
			func(key string) string {
				if key == "LOCAL_ARTIFACT_BIN_DIR" {
					return "~/bin"
				}
				return ""
			},
			func(string) (string, error) { return "", errors.New("missing") },
		)
		if got != configured {
			t.Fatalf("ccsubagentsdPath() = %q, want %q", got, configured)
		}
	})

	t.Run("uses lookPath when local candidates missing", func(t *testing.T) {
		found := "/opt/tools/ccsubagentsd"
		got := ccsubagentsdPath(filepath.Join(t.TempDir(), "local-artifact-mcp"), t.TempDir(), "linux",
			func(string) string { return "" },
			func(string) (string, error) { return found, nil },
		)
		if got != found {
			t.Fatalf("ccsubagentsdPath() = %q, want %q", got, found)
		}
	})

	t.Run("uses windows suffix", func(t *testing.T) {
		found := `C:\Tools\ccsubagentsd.exe`
		got := ccsubagentsdPath(filepath.Join(t.TempDir(), "local-artifact-mcp.exe"), t.TempDir(), "windows",
			func(string) string { return "" },
			func(name string) (string, error) {
				if name != "ccsubagentsd.exe" {
					t.Fatalf("lookPath called with %q", name)
				}
				return found, nil
			},
		)
		if got != found {
			t.Fatalf("ccsubagentsdPath() = %q, want %q", got, found)
		}
	})
}

type fakeDaemonReadinessProber struct {
	healthErr error
	listErr   error
	listReqs  []daemon.ListRequest
}

func (f *fakeDaemonReadinessProber) Health(_ context.Context) error {
	return f.healthErr
}

func (f *fakeDaemonReadinessProber) List(_ context.Context, req daemon.ListRequest) (daemon.ListResponse, error) {
	f.listReqs = append(f.listReqs, req)
	if f.listErr != nil {
		return daemon.ListResponse{}, f.listErr
	}
	return daemon.ListResponse{}, nil
}

func TestDaemonReady_HealthFailureSkipsAuthenticatedProbe(t *testing.T) {
	fake := &fakeDaemonReadinessProber{healthErr: errors.New("health down")}
	err := daemonReady(context.Background(), fake)
	if err == nil || err.Error() != "health down" {
		t.Fatalf("expected health error, got %v", err)
	}
	if len(fake.listReqs) != 0 {
		t.Fatalf("expected no list probe when health fails, got %d calls", len(fake.listReqs))
	}
}

func TestDaemonReady_IncludesAuthenticatedProbe(t *testing.T) {
	unauthorized := &daemon.RemoteError{Code: daemon.CodeUnauthorized, Message: "missing or invalid token"}
	fake := &fakeDaemonReadinessProber{listErr: unauthorized}
	err := daemonReady(context.Background(), fake)
	if !errors.Is(err, unauthorized) {
		t.Fatalf("expected unauthorized readiness error, got %v", err)
	}
	if len(fake.listReqs) != 1 {
		t.Fatalf("expected one authenticated list probe, got %d", len(fake.listReqs))
	}
	req := fake.listReqs[0]
	if req.Workspace.WorkspaceID != workspaces.GlobalWorkspaceID {
		t.Fatalf("expected global workspace probe, got %+v", req.Workspace)
	}
	if req.Limit != 1 {
		t.Fatalf("expected readiness list limit=1, got %d", req.Limit)
	}
}

func TestRunWithAutostartedWebChild_CleansUpOnRunFailure(t *testing.T) {
	resetMCPMainHooks(t)
	child := &childProcess{}
	cleanupCalled := false

	startLocalArtifactWebFn = func(io.Writer) (*childProcess, error) {
		return child, nil
	}
	stopChildProcessFn = func(got *childProcess, timeout time.Duration) error {
		if got != child {
			t.Fatalf("stopChildProcess called with unexpected child")
		}
		if timeout != 2*time.Second {
			t.Fatalf("stopChildProcess timeout=%v, want %v", timeout, 2*time.Second)
		}
		cleanupCalled = true
		return nil
	}

	runErr := errors.New("daemon readiness failed")
	err := runWithAutostartedWebChild(true, io.Discard, func() error {
		return runErr
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("runWithAutostartedWebChild() error = %v, want %v", err, runErr)
	}
	if !cleanupCalled {
		t.Fatalf("expected autostarted web child cleanup on run failure")
	}
}

func TestStopChildProcess_GracefulErrorFallsBackToForce(t *testing.T) {
	resetMCPMainHooks(t)
	exited := make(chan error, 1)
	child := &childProcess{
		cmd:    &exec.Cmd{Process: &os.Process{Pid: 999999}},
		exited: exited,
	}

	gracefulCalled := false
	forceCalled := false
	sendChildGracefulFn = func(proc *os.Process) error {
		if proc != child.cmd.Process {
			t.Fatalf("graceful called with unexpected process")
		}
		gracefulCalled = true
		return errors.New("interrupt not supported")
	}
	sendChildForceFn = func(proc *os.Process) error {
		if proc != child.cmd.Process {
			t.Fatalf("force called with unexpected process")
		}
		forceCalled = true
		exited <- nil
		close(exited)
		return nil
	}

	err := stopChildProcess(child, 0)
	if err != nil {
		t.Fatalf("stopChildProcess() returned error: %v", err)
	}
	if !gracefulCalled {
		t.Fatalf("expected graceful stop attempt")
	}
	if !forceCalled {
		t.Fatalf("expected force stop fallback after graceful error")
	}
}

func resetMCPMainHooks(t *testing.T) {
	t.Helper()
	origStartLocalArtifactWebFn := startLocalArtifactWebFn
	origStopChildProcessFn := stopChildProcessFn
	origSendChildGracefulFn := sendChildGracefulFn
	origSendChildForceFn := sendChildForceFn
	t.Cleanup(func() {
		startLocalArtifactWebFn = origStartLocalArtifactWebFn
		stopChildProcessFn = origStopChildProcessFn
		sendChildGracefulFn = origSendChildGracefulFn
		sendChildForceFn = origSendChildForceFn
	})
}
