package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestHandleToolsCall_SchemaValidationRejectsUnknownFields(t *testing.T) {
	s := newDaemonBackedServer(t)
	s.setInitialized(true)

	respAny, rpcErr := s.handleToolsCall(context.Background(), mustRawJSON(t, map[string]any{
		"name": toolArtifactSaveText,
		"arguments": map[string]any{
			"name":  "plan/schema-unknown",
			"text":  "ok",
			"extra": true,
		},
	}))
	if rpcErr != nil {
		t.Fatalf("tools/call rpc error: %+v", rpcErr)
	}
	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected schema validation error, got %+v", resp)
	}
	msg := firstContentText(resp)
	if !strings.HasPrefix(msg, "Invalid arguments:") {
		t.Fatalf("unexpected validation message: %q", msg)
	}
}

func TestHandleToolsCall_SchemaValidationRejectsMissingRequiredField(t *testing.T) {
	s := newDaemonBackedServer(t)
	s.setInitialized(true)

	respAny, rpcErr := s.handleToolsCall(context.Background(), mustRawJSON(t, map[string]any{
		"name": toolArtifactSaveText,
		"arguments": map[string]any{
			"name": "plan/schema-missing",
		},
	}))
	if rpcErr != nil {
		t.Fatalf("tools/call rpc error: %+v", rpcErr)
	}
	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected schema validation error, got %+v", resp)
	}
	msg := firstContentText(resp)
	if !strings.HasPrefix(msg, "Invalid arguments:") {
		t.Fatalf("unexpected validation message: %q", msg)
	}
}
