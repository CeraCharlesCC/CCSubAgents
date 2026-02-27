package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
)

type WorkspaceRegistry struct {
	db *sql.DB
}

func NewWorkspaceRegistry(baseRoot string) (*WorkspaceRegistry, error) {
	db, err := OpenRegistryDB(baseRoot)
	if err != nil {
		return nil, err
	}
	return &WorkspaceRegistry{db: db}, nil
}

func (r *WorkspaceRegistry) EnsureWorkspace(ctx context.Context, workspaceID string, roots []string, owner string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = workspaces.GlobalWorkspaceID
	}
	if roots == nil {
		roots = []string{}
	}
	cleanRoots := append([]string(nil), roots...)
	sort.Strings(cleanRoots)
	payload, err := json.Marshal(cleanRoots)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO workspaces(workspace_id, roots_json, owner, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET
			roots_json = excluded.roots_json,
			owner = excluded.owner,
			last_seen_at = excluded.last_seen_at;
	`, workspaceID, string(payload), strings.TrimSpace(owner), now, now)
	return err
}

func (r *WorkspaceRegistry) ListWorkspaces(ctx context.Context) ([]workspaces.Workspace, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT workspace_id, roots_json, owner, created_at, last_seen_at
		FROM workspaces
		ORDER BY workspace_id ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]workspaces.Workspace, 0)
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *WorkspaceRegistry) GetWorkspace(ctx context.Context, workspaceID string) (workspaces.Workspace, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT workspace_id, roots_json, owner, created_at, last_seen_at
		FROM workspaces
		WHERE workspace_id = ?;
	`, strings.TrimSpace(workspaceID))
	ws, err := scanWorkspaceRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workspaces.Workspace{}, artifacts.ErrNotFound
		}
		return workspaces.Workspace{}, err
	}
	return ws, nil
}

func scanWorkspace(rows *sql.Rows) (workspaces.Workspace, error) {
	var (
		workspaceID string
		rootsJSON   string
		owner       string
		createdAt   string
		lastSeenAt  string
	)
	if err := rows.Scan(&workspaceID, &rootsJSON, &owner, &createdAt, &lastSeenAt); err != nil {
		return workspaces.Workspace{}, err
	}
	return buildWorkspace(workspaceID, rootsJSON, owner, createdAt, lastSeenAt)
}

func scanWorkspaceRow(row *sql.Row) (workspaces.Workspace, error) {
	var (
		workspaceID string
		rootsJSON   string
		owner       string
		createdAt   string
		lastSeenAt  string
	)
	if err := row.Scan(&workspaceID, &rootsJSON, &owner, &createdAt, &lastSeenAt); err != nil {
		return workspaces.Workspace{}, err
	}
	return buildWorkspace(workspaceID, rootsJSON, owner, createdAt, lastSeenAt)
}

func buildWorkspace(workspaceID, rootsJSON, owner, createdAt, lastSeenAt string) (workspaces.Workspace, error) {
	roots := []string{}
	if strings.TrimSpace(rootsJSON) != "" {
		if err := json.Unmarshal([]byte(rootsJSON), &roots); err != nil {
			return workspaces.Workspace{}, err
		}
	}
	created, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return workspaces.Workspace{}, err
	}
	lastSeen, err := time.Parse(time.RFC3339, lastSeenAt)
	if err != nil {
		return workspaces.Workspace{}, err
	}
	return workspaces.Workspace{
		WorkspaceID: workspaceID,
		Roots:       roots,
		Owner:       owner,
		CreatedAt:   created,
		LastSeenAt:  lastSeen,
	}, nil
}
