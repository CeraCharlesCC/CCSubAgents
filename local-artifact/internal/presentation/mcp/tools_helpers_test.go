package mcp

import (
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func TestToolErrorFromErr_DaemonUnauthorized(t *testing.T) {
	result := toolErrorFromErr(&daemon.RemoteError{Code: daemon.CodeUnauthorized, Message: "missing or invalid token"})
	if !result.IsError {
		t.Fatalf("expected tool error, got %+v", result)
	}
	msg := firstContentText(result)
	if !strings.Contains(msg, "unauthorized") {
		t.Fatalf("expected unauthorized prefix, got %q", msg)
	}
	if !strings.Contains(msg, "missing or invalid token") {
		t.Fatalf("expected daemon auth message, got %q", msg)
	}
}
