package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestWorkspaceRegistry_EnsureAndList(t *testing.T) {
	registry, err := NewWorkspaceRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	ctx := context.Background()

	if err := registry.EnsureWorkspace(ctx, "global", []string{"file:///repo/a"}, "mcp"); err != nil {
		t.Fatalf("ensure global: %v", err)
	}
	first, err := registry.GetWorkspace(ctx, "global")
	if err != nil {
		t.Fatalf("get global: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := registry.EnsureWorkspace(ctx, "global", []string{"file:///repo/b"}, "web"); err != nil {
		t.Fatalf("ensure global update: %v", err)
	}
	updated, err := registry.GetWorkspace(ctx, "global")
	if err != nil {
		t.Fatalf("get global after update: %v", err)
	}
	if updated.Owner != "web" {
		t.Fatalf("expected owner update to web, got %q", updated.Owner)
	}
	if len(updated.Roots) != 1 || updated.Roots[0] != "file:///repo/b" {
		t.Fatalf("unexpected roots: %+v", updated.Roots)
	}
	if updated.LastSeenAt.Before(first.LastSeenAt) {
		t.Fatalf("expected last_seen_at to stay same or move forward")
	}

	if err := registry.EnsureWorkspace(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []string{}, "mcp"); err != nil {
		t.Fatalf("ensure hash workspace: %v", err)
	}
	items, err := registry.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(items))
	}
	if items[0].WorkspaceID > items[1].WorkspaceID {
		t.Fatalf("expected deterministic ordering by workspace_id, got %q then %q", items[0].WorkspaceID, items[1].WorkspaceID)
	}
}
