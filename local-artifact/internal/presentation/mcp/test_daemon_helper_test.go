package mcp

import (
	"net/http/httptest"
	"testing"

	daemonapi "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func newDaemonBackedServer(t *testing.T) *Server {
	t.Helper()
	return newDaemonBackedServerAtRoot(t, t.TempDir())
}

func newDaemonBackedServerAtRoot(t *testing.T, storeRoot string) *Server {
	t.Helper()
	engine, err := daemonapi.NewEngine(storeRoot)
	if err != nil {
		t.Fatalf("new daemon engine: %v", err)
	}
	t.Cleanup(func() {
		_ = engine.Close()
	})

	h := httptest.NewServer(daemonapi.NewServer(engine, "mcp-test").Routes())
	t.Cleanup(h.Close)

	client := daemonapi.NewHTTPClient(h.URL, "")
	return NewWithClient(storeRoot, client)
}
