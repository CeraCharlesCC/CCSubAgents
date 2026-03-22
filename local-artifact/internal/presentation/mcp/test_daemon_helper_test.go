package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
	daemonapi "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemonapi"
)

func newDaemonBackedServer(t *testing.T) *Server {
	t.Helper()
	return newDaemonBackedServerAtRoot(t, t.TempDir())
}

func newDaemonBackedServerAtRoot(t *testing.T, storeRoot string) *Server {
	t.Helper()
	engine, err := daemon.NewEngine(storeRoot)
	if err != nil {
		t.Fatalf("new daemon engine: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := engine.Close(); closeErr != nil {
			t.Fatalf("close daemon engine: %v", closeErr)
		}
	})

	h := httptest.NewServer(daemon.NewServer(engine, "mcp-test").Routes())
	t.Cleanup(h.Close)

	client := daemonapi.NewHTTPClient(h.URL, "")
	return NewWithClient(storeRoot, client)
}
