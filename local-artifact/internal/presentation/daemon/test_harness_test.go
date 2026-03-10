package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
)

type daemonHTTPHarness struct {
	client    *Client
	workspace WorkspaceSelector
	ctx       context.Context

	httpServer *httptest.Server
}

func newDaemonEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := engine.Close(); closeErr != nil {
			t.Fatalf("close engine: %v", closeErr)
		}
	})
	return engine
}

func newDaemonHTTPHarness(t *testing.T) *daemonHTTPHarness {
	t.Helper()
	engine := newDaemonEngine(t)
	httpServer := httptest.NewServer(NewServer(engine, "test").Routes())
	t.Cleanup(httpServer.Close)

	return &daemonHTTPHarness{
		client:     NewHTTPClient(httpServer.URL, ""),
		workspace:  WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID},
		ctx:        context.Background(),
		httpServer: httpServer,
	}
}

func decodeEnvelope(t *testing.T, raw []byte) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v body=%q", err, string(raw))
	}
	return env
}
