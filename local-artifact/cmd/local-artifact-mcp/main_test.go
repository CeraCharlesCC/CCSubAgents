package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
			want:    filepath.Join(base, "ccsubagentsd"),
		},
		{
			name:    "windows",
			goos:    "windows",
			exePath: filepath.Join(base, "local-artifact-mcp.exe"),
			want:    filepath.Join(base, "ccsubagentsd.exe"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ccsubagentsdPath(tc.exePath, tc.goos)
			if got != tc.want {
				t.Fatalf("ccsubagentsdPath() = %q, want %q", got, tc.want)
			}
		})
	}
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
