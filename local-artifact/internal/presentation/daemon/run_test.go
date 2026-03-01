package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
)

func TestResolveLoopbackTCPAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{name: "default", addr: "", wantErr: false},
		{name: "localhost", addr: "localhost:19131", wantErr: false},
		{name: "ipv4 loopback", addr: "127.0.0.1:19131", wantErr: false},
		{name: "ipv6 loopback", addr: "[::1]:19131", wantErr: false},
		{name: "non loopback ipv4", addr: "0.0.0.0:19131", wantErr: true},
		{name: "public ip", addr: "8.8.8.8:19131", wantErr: true},
		{name: "bad format", addr: "not-a-hostport", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveLoopbackTCPAddr(tc.addr)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.addr)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.addr, err)
			}
		})
	}
}

func TestDaemonWebServiceResolver_RegistersDaemonOwnership(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	resolver := daemonWebServiceResolver(engine)
	if _, err := resolver("global"); err != nil {
		t.Fatalf("resolve global service: %v", err)
	}

	workspace, err := engine.registry.GetWorkspace(context.Background(), workspaces.GlobalWorkspaceID)
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	if workspace.Owner != "daemon" {
		t.Fatalf("workspace owner mismatch: got=%q want=%q", workspace.Owner, "daemon")
	}
}

func TestWebBootstrapHint_RedactsTokenValue(t *testing.T) {
	storeRoot := "/tmp/example"
	hint := webBootstrapHint("127.0.0.1:19130", storeRoot)
	if strings.Contains(hint, "token=") {
		t.Fatalf("bootstrap hint should not include token query, got %q", hint)
	}
	if !strings.Contains(hint, tokenFilePath(storeRoot)) {
		t.Fatalf("bootstrap hint should reference token file path, got %q", hint)
	}
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

type daemonRunFixture struct {
	StoreRoot string
	StateDir  string
	Socket    string
}

func newDaemonRunFixture(t *testing.T, socketPrefix string) daemonRunFixture {
	t.Helper()
	root := t.TempDir()
	return daemonRunFixture{
		StoreRoot: filepath.Join(root, "store"),
		StateDir:  filepath.Join(root, "state"),
		Socket:    tempUnixSocketPath(t, socketPrefix),
	}
}

func startDaemonAsync(cfg RunConfig) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), cfg)
	}()
	return errCh
}

func waitForDaemonStop(t *testing.T, errCh <-chan error, name string) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not stop before timeout", name)
	}
}

func TestCleanupStaleSocket_RemovesStaleSocketOnConnRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	socket := tempUnixSocketPath(t, "ccsubagentsd-cleanup-")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close unix listener: %v", err)
	}

	if err := cleanupStaleSocket(socket); err != nil {
		t.Fatalf("cleanup stale socket: %v", err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("expected socket to be removed, stat err=%v", err)
	}
}

func TestCleanupStaleSocket_RemovesStaleSocketOnDialENOENT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	socket := tempUnixSocketPath(t, "ccsubagentsd-cleanup-")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close unix listener: %v", err)
	}

	origDialTimeoutFn := dialTimeoutFn
	dialTimeoutFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ENOENT}
	}
	t.Cleanup(func() {
		dialTimeoutFn = origDialTimeoutFn
	})

	if err := cleanupStaleSocket(socket); err != nil {
		t.Fatalf("cleanup stale socket: %v", err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("expected socket to be removed, stat err=%v", err)
	}
}

func TestCleanupStaleSocket_DoesNotRemoveNonSocketFile(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(socket, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	err := cleanupStaleSocket(socket)
	if err == nil {
		t.Fatal("expected cleanup to reject non-socket path")
	}
	if _, statErr := os.Stat(socket); statErr != nil {
		t.Fatalf("expected regular file to remain, stat err=%v", statErr)
	}
}

func TestCleanupStaleSocket_AlreadyListeningDoesNotRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	socket := tempUnixSocketPath(t, "ccsubagentsd-cleanup-")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})

	err = cleanupStaleSocket(socket)
	if err == nil {
		t.Fatal("expected apiAlreadyListeningError")
	}
	var alreadyErr *apiAlreadyListeningError
	if !errors.As(err, &alreadyErr) {
		t.Fatalf("expected apiAlreadyListeningError, got %v", err)
	}
	if _, statErr := os.Stat(socket); statErr != nil {
		t.Fatalf("expected socket to remain, stat err=%v", statErr)
	}
}

func TestRun_ShutdownEndpointStopsDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	fixture := newDaemonRunFixture(t, "ccsubagentsd-test-")
	token := "test-shutdown-token"

	errCh := startDaemonAsync(RunConfig{
		StoreRoot: fixture.StoreRoot,
		StateDir:  fixture.StateDir,
		APISocket: fixture.Socket,
		Token:     token,
		Stderr:    io.Discard,
	})

	client := NewUnixSocketClient(fixture.Socket, token)
	deadline := time.Now().Add(10 * time.Second)
	for {
		healthCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		healthErr := client.Health(healthCtx)
		cancel()
		if healthErr == nil {
			break
		}
		select {
		case runErr := <-errCh:
			t.Fatalf("daemon exited before healthy: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not become healthy before timeout")
		}
		time.Sleep(75 * time.Millisecond)
	}

	if _, err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown request failed: %v", err)
	}
	waitForDaemonStop(t, errCh, "daemon")
}

func TestRun_WebOnlyModeWhenAPISocketAlreadyActive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	fixture := newDaemonRunFixture(t, "ccsubagentsd-webonly-")

	primaryErrCh := startDaemonAsync(RunConfig{
		StoreRoot:   fixture.StoreRoot,
		StateDir:    fixture.StateDir,
		APISocket:   fixture.Socket,
		DisableAuth: true,
		Stderr:      io.Discard,
	})

	client := NewUnixSocketClient(fixture.Socket, "")
	waitForDaemonReady(t, client, primaryErrCh)

	webAddr := reserveLoopbackAddr(t)
	secondaryErrCh := startDaemonAsync(RunConfig{
		StoreRoot:   fixture.StoreRoot,
		StateDir:    fixture.StateDir,
		APISocket:   fixture.Socket,
		WebAddr:     webAddr,
		DisableAuth: true,
		Stderr:      io.Discard,
	})

	waitForWebHealth(t, webAddr)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+webAddr+"/daemon/v1/control/shutdown", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build shutdown request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("shutdown web-only daemon: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("shutdown status mismatch: got=%d want=%d", resp.StatusCode, http.StatusAccepted)
	}
	waitForDaemonStop(t, secondaryErrCh, "web-only daemon")

	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("primary daemon should still be healthy after web-only shutdown: %v", err)
	}

	if _, err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown primary daemon: %v", err)
	}
	waitForDaemonStop(t, primaryErrCh, "primary daemon")
}

func TestRun_WithoutWebAddrStillErrorsWhenAPISocketAlreadyActive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	fixture := newDaemonRunFixture(t, "ccsubagentsd-api-busy-")

	primaryErrCh := startDaemonAsync(RunConfig{
		StoreRoot:   fixture.StoreRoot,
		StateDir:    fixture.StateDir,
		APISocket:   fixture.Socket,
		DisableAuth: true,
		Stderr:      io.Discard,
	})

	client := NewUnixSocketClient(fixture.Socket, "")
	waitForDaemonReady(t, client, primaryErrCh)

	err := Run(context.Background(), RunConfig{
		StoreRoot:   fixture.StoreRoot,
		StateDir:    fixture.StateDir,
		APISocket:   fixture.Socket,
		DisableAuth: true,
		Stderr:      io.Discard,
	})
	var listeningErr *apiAlreadyListeningError
	if !errors.As(err, &listeningErr) {
		t.Fatalf("expected apiAlreadyListeningError, got %v", err)
	}

	if _, err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown primary daemon: %v", err)
	}
	waitForDaemonStop(t, primaryErrCh, "primary daemon")
}

func waitForDaemonReady(t *testing.T, client *Client, daemonErrCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		healthCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		healthErr := client.Health(healthCtx)
		cancel()
		if healthErr == nil {
			return
		}
		select {
		case runErr := <-daemonErrCh:
			t.Fatalf("daemon exited before healthy: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not become healthy before timeout: %v", healthErr)
		}
		time.Sleep(75 * time.Millisecond)
	}
}

func waitForWebHealth(t *testing.T, webAddr string) {
	t.Helper()
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+webAddr+"/daemon/v1/health", nil)
		if err != nil {
			t.Fatalf("build health request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("web daemon did not become healthy before timeout: %v", err)
			}
			t.Fatalf("web daemon health status mismatch before timeout: got=%d want=%d", resp.StatusCode, http.StatusOK)
		}
		time.Sleep(75 * time.Millisecond)
	}
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback addr: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close reserved listener: %v", err)
	}
	return addr
}
