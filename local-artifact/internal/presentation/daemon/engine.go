package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/migrate"
	artsqlite "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/sqlite"
)

type serviceEntry struct {
	service *artifacts.Service
	closeFn func() error
}

type Engine struct {
	baseStoreRoot string
	registry      workspaces.Registry

	mu      sync.Mutex
	service map[string]serviceEntry
}

func NewEngine(baseStoreRoot string) (*Engine, error) {
	registry, err := artsqlite.NewWorkspaceRegistry(baseStoreRoot)
	if err != nil {
		return nil, err
	}
	return &Engine{
		baseStoreRoot: baseStoreRoot,
		registry:      registry,
		service:       map[string]serviceEntry{},
	}, nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var firstErr error
	for _, entry := range e.service {
		if entry.closeFn == nil {
			continue
		}
		if err := entry.closeFn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) resolveWorkspace(ctx context.Context, sel WorkspaceSelector, owner string) (string, *artifacts.Service, error) {
	workspaceID, roots, err := normalizeWorkspaceSelector(sel)
	if err != nil {
		return "", nil, err
	}
	if err := e.registry.EnsureWorkspace(ctx, workspaceID, roots, owner); err != nil {
		return "", nil, err
	}
	svc, err := e.serviceForWorkspaceID(ctx, workspaceID)
	if err != nil {
		return "", nil, err
	}
	return workspaceID, svc, nil
}

func normalizeWorkspaceSelector(sel WorkspaceSelector) (string, []string, error) {
	workspaceID := strings.ToLower(strings.TrimSpace(sel.WorkspaceID))
	hasWorkspaceID := workspaceID != ""
	hasRoots := len(sel.Roots) > 0
	if hasWorkspaceID == hasRoots {
		return "", nil, fmt.Errorf("%w: workspace must provide exactly one of workspaceID or roots", artifacts.ErrInvalidInput)
	}

	if hasRoots {
		normalized, err := workspaces.NormalizeRootURIs(sel.Roots)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", artifacts.ErrInvalidInput, err)
		}
		return workspaces.ComputeWorkspaceID(normalized), normalized, nil
	}

	if workspaceID == workspaces.GlobalWorkspaceID {
		return workspaceID, []string{}, nil
	}
	if len(workspaceID) != 64 {
		return "", nil, fmt.Errorf("%w: workspaceID must be global or 64 lowercase hex", artifacts.ErrInvalidInput)
	}
	if _, err := hex.DecodeString(workspaceID); err != nil {
		return "", nil, fmt.Errorf("%w: workspaceID must be lowercase hex", artifacts.ErrInvalidInput)
	}
	if workspaceID != strings.ToLower(workspaceID) {
		return "", nil, fmt.Errorf("%w: workspaceID must be lowercase hex", artifacts.ErrInvalidInput)
	}
	return workspaceID, []string{}, nil
}

func (e *Engine) serviceForWorkspaceID(ctx context.Context, workspaceID string) (*artifacts.Service, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = workspaces.GlobalWorkspaceID
	}

	e.mu.Lock()
	if entry, ok := e.service[workspaceID]; ok {
		e.mu.Unlock()
		return entry.service, nil
	}
	e.mu.Unlock()

	workspaceRoot := e.baseStoreRoot
	if workspaceID != workspaces.GlobalWorkspaceID {
		workspaceRoot = filepath.Join(e.baseStoreRoot, workspaceID)
	}
	if _, err := migrate.MigrateWorkspaceIfNeeded(ctx, workspaceRoot); err != nil {
		return nil, err
	}
	repo, err := artsqlite.NewArtifactRepository(workspaceRoot)
	if err != nil {
		return nil, err
	}
	entry := serviceEntry{
		service: artifacts.NewService(repo),
		closeFn: repo.Close,
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.service[workspaceID]; ok {
		_ = repo.Close()
		return existing.service, nil
	}
	e.service[workspaceID] = entry
	return entry.service, nil
}
