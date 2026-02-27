package workspaces

import (
	"context"
	"time"
)

type Workspace struct {
	WorkspaceID string
	Roots       []string
	Owner       string
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

type Registry interface {
	EnsureWorkspace(ctx context.Context, workspaceID string, roots []string, owner string) error
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error)
}
