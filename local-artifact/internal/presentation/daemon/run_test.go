package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestRun_ShutdownEndpointStopsDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	storeRoot := filepath.Join(t.TempDir(), "store")
	stateDir := filepath.Join(t.TempDir(), "state")
	socketDir, err := os.MkdirTemp("/tmp", "ccsubagentsd-test-")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(socketDir)
	})
	socket := filepath.Join(socketDir, "daemon.sock")
	token := "test-shutdown-token"

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), RunConfig{
			StoreRoot: storeRoot,
			StateDir:  stateDir,
			APISocket: socket,
			Token:     token,
			Stderr:    io.Discard,
		})
	}()

	client := NewUnixSocketClient(socket, token)
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

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon run returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop after shutdown request")
	}
}
