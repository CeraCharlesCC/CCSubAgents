package installer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	pathslib "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

func writeDaemonErrorEnvelope(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func startShutdownServiceUnavailableDaemon(t *testing.T, socketPath string, shutdownMessage string) *atomic.Int32 {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("create socket parent dir: %v", err)
	}
	_ = os.Remove(socketPath)

	server := &http.Server{}
	var healthCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon/v1/control/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeDaemonErrorEnvelope(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		writeDaemonErrorEnvelope(w, http.StatusServiceUnavailable, daemonclient.CodeServiceUnavailable, shutdownMessage)
		go func() {
			_ = server.Close()
			_ = os.Remove(socketPath)
		}()
	})
	mux.HandleFunc("/daemon/v1/health", func(w http.ResponseWriter, r *http.Request) {
		healthCalls.Add(1)
		writeDaemonErrorEnvelope(w, http.StatusServiceUnavailable, daemonclient.CodeServiceUnavailable, "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory")
	})
	server.Handler = mux

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})

	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	return &healthCalls
}

func tempUnixSocketPath(t *testing.T, prefix string) string {
	t.Helper()
	socketDir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(socketDir)
	})
	return filepath.Join(socketDir, "daemon.sock")
}

func TestUninstallLocal_StopsDaemonBeforeDeletingManagedFiles(t *testing.T) {
	home := t.TempDir()
	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	managedDir := filepath.Join(installRoot, config.LocalManagedDirRelativePath)
	if err := os.MkdirAll(managedDir, stateDirPerm); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	managedFile := filepath.Join(managedDir, ccsubagentsdBinaryName(runtime.GOOS))
	if err := os.WriteFile(managedFile, []byte("daemon"), stateFilePerm); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}

	if err := state.SaveTrackedState(stateDir, state.TrackedState{
		Version: state.TrackedSchemaVersion,
		Local: []state.LocalInstall{{
			InstallRoot: installRoot,
			Mode:        state.LocalInstallModePersonal,
			Repo:        release.Repo,
			ReleaseID:   1,
			ReleaseTag:  "v1.0.0",
			InstalledAt: "2026-01-01T00:00:00Z",
			Managed: state.ManagedState{
				Files: []string{managedFile},
				Dirs:  []string{managedDir},
			},
		}},
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	called := false
	m := &Runner{
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return installRoot, nil },
		getenv: func(key string) string {
			if key == pathslib.EnvStateDir {
				return stateDir
			}
			return ""
		},
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("not a git repository")
		},
		stopDaemonFn: func(context.Context) error {
			called = true
			if _, err := os.Stat(managedFile); err != nil {
				t.Fatalf("expected managed file to exist before daemon stop hook: %v", err)
			}
			return nil
		},
	}

	if err := m.uninstallLocal(context.Background()); err != nil {
		t.Fatalf("uninstallLocal should succeed: %v", err)
	}
	if !called {
		t.Fatalf("expected stopDaemonFn to be called")
	}
	if _, err := os.Stat(managedFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected managed file removed after uninstallLocal, stat err: %v", err)
	}
}

func TestUninstallLocal_StopDaemonFailure_PreventsDeletion(t *testing.T) {
	home := t.TempDir()
	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	managedDir := filepath.Join(installRoot, config.LocalManagedDirRelativePath)
	if err := os.MkdirAll(managedDir, stateDirPerm); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	managedFile := filepath.Join(managedDir, ccsubagentsdBinaryName(runtime.GOOS))
	if err := os.WriteFile(managedFile, []byte("daemon"), stateFilePerm); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}

	if err := state.SaveTrackedState(stateDir, state.TrackedState{
		Version: state.TrackedSchemaVersion,
		Local: []state.LocalInstall{{
			InstallRoot: installRoot,
			Mode:        state.LocalInstallModePersonal,
			Repo:        release.Repo,
			ReleaseID:   1,
			ReleaseTag:  "v1.0.0",
			InstalledAt: "2026-01-01T00:00:00Z",
			Managed: state.ManagedState{
				Files: []string{managedFile},
				Dirs:  []string{managedDir},
			},
		}},
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	m := &Runner{
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return installRoot, nil },
		getenv: func(key string) string {
			if key == pathslib.EnvStateDir {
				return stateDir
			}
			return ""
		},
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("not a git repository")
		},
		stopDaemonFn: func(context.Context) error {
			return errors.New("stop failed")
		},
	}

	err := m.uninstallLocal(context.Background())
	if err == nil {
		t.Fatalf("expected uninstallLocal failure when daemon stop fails")
	}
	if !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("expected stop failure in uninstallLocal error, got %v", err)
	}
	if _, statErr := os.Stat(managedFile); statErr != nil {
		t.Fatalf("expected managed file to remain when daemon stop fails, stat err: %v", statErr)
	}
}

func TestUninstallLocal_NonStoppedServiceUnavailable_PreventsDeletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based daemon client test")
	}

	home := t.TempDir()
	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	managedDir := filepath.Join(installRoot, config.LocalManagedDirRelativePath)
	if err := os.MkdirAll(managedDir, stateDirPerm); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	managedFile := filepath.Join(managedDir, ccsubagentsdBinaryName(runtime.GOOS))
	if err := os.WriteFile(managedFile, []byte("daemon"), stateFilePerm); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}

	if err := state.SaveTrackedState(stateDir, state.TrackedState{
		Version: state.TrackedSchemaVersion,
		Local: []state.LocalInstall{{
			InstallRoot: installRoot,
			Mode:        state.LocalInstallModePersonal,
			Repo:        release.Repo,
			ReleaseID:   1,
			ReleaseTag:  "v1.0.0",
			InstalledAt: "2026-01-01T00:00:00Z",
			Managed: state.ManagedState{
				Files: []string{managedFile},
				Dirs:  []string{managedDir},
			},
		}},
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	socketPath := tempUnixSocketPath(t, "ccsubagents-uninstall-")
	healthCalls := startShutdownServiceUnavailableDaemon(t, socketPath, "dial unix /tmp/ccsubagentsd.sock: connect: network is unreachable")

	m := &Runner{
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return installRoot, nil },
		getenv: func(key string) string {
			switch key {
			case pathslib.EnvStateDir:
				return stateDir
			case "LOCAL_ARTIFACT_DAEMON_SOCKET":
				return socketPath
			default:
				return ""
			}
		},
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("not a git repository")
		},
	}

	err := m.uninstallLocal(context.Background())
	if err == nil {
		t.Fatalf("expected uninstallLocal failure for non-stopped service unavailable")
	}
	if !strings.Contains(err.Error(), "shutdown daemon") {
		t.Fatalf("expected shutdown context in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "network is unreachable") {
		t.Fatalf("expected non-stopped service unavailable message, got %v", err)
	}
	if _, statErr := os.Stat(managedFile); statErr != nil {
		t.Fatalf("expected managed file to remain when shutdown fails, stat err: %v", statErr)
	}
	if healthCalls.Load() != 0 {
		t.Fatalf("expected uninstall to abort before stop verification, health calls=%d", healthCalls.Load())
	}
}
