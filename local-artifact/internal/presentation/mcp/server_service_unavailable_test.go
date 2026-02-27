package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func TestHandleToolsCall_WorkspaceServiceInitFailureReturnsTypedToolError(t *testing.T) {
	s := NewWithClient(t.TempDir(), daemon.NewUnavailableClient(errors.New("boom")))
	s.setInitialized(true)

	respAny, rpcErr := s.handleToolsCall(context.Background(), mustRawJSON(t, map[string]any{
		"name": toolArtifactResolve,
		"arguments": map[string]any{
			"name": "plan/task-unavailable",
		},
	}))
	if rpcErr != nil {
		t.Fatalf("tools/call rpc error: %+v", rpcErr)
	}

	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected tool error when workspace service init fails, got %+v", resp)
	}
	msg := firstContentText(resp)
	if !strings.Contains(msg, "internal error") {
		t.Fatalf("expected internal error prefix, got %q", msg)
	}
	if !strings.Contains(msg, "service unavailable") {
		t.Fatalf("expected typed unavailable-service error, got %q", msg)
	}
}
