package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/blobstore"
)

type ArtifactRepository struct {
	workspaceRoot string
	db            *sql.DB
	blobs         *blobstore.Store
}

func NewArtifactRepository(workspaceRoot string) (*ArtifactRepository, error) {
	db, err := OpenMetaDB(workspaceRoot)
	if err != nil {
		return nil, err
	}
	return &ArtifactRepository{
		workspaceRoot: workspaceRoot,
		db:            db,
		blobs:         blobstore.New(filepath.Join(workspaceRoot, "blobs")),
	}, nil
}

func (r *ArtifactRepository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *ArtifactRepository) Save(ctx context.Context, a artifacts.ArtifactVersion, data []byte, opts artifacts.SaveOptions) (artifacts.ArtifactVersion, error) {
	if data == nil {
		data = []byte{}
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC().Truncate(time.Second)
	}

	if !a.Tombstone {
		if strings.TrimSpace(a.SHA256) == "" {
			return artifacts.ArtifactVersion{}, fmt.Errorf("%w: sha256 is required", artifacts.ErrInvalidInput)
		}
		if err := r.blobs.Put(a.SHA256, data); err != nil {
			return artifacts.ArtifactVersion{}, err
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		out, err := r.saveOnce(ctx, a, opts)
		if err == nil {
			return out, nil
		}
		if !isRetryableBusyErr(err) {
			return artifacts.ArtifactVersion{}, err
		}
		lastErr = err
		time.Sleep(time.Duration(10+attempt*20) * time.Millisecond)
	}
	if lastErr != nil {
		return artifacts.ArtifactVersion{}, lastErr
	}
	return artifacts.ArtifactVersion{}, fmt.Errorf("%w: save failed", artifacts.ErrInternal)
}

func (r *ArtifactRepository) saveOnce(ctx context.Context, a artifacts.ArtifactVersion, opts artifacts.SaveOptions) (artifacts.ArtifactVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	defer rollbackIgnore(tx)

	now := a.CreatedAt.UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts(name, latest_version_id, deleted, created_at, updated_at, deleted_at)
		VALUES (?, NULL, 0, ?, ?, NULL)
		ON CONFLICT(name) DO NOTHING;
	`, a.Name, now, now); err != nil {
		return artifacts.ArtifactVersion{}, err
	}

	var currentLatest sql.NullString
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT latest_version_id, deleted FROM artifacts WHERE name = ?;`, a.Name).Scan(&currentLatest, &deleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
		}
		return artifacts.ArtifactVersion{}, err
	}

	expected := strings.TrimSpace(opts.ExpectedPrevRef)
	if expected != "" {
		current := strings.TrimSpace(currentLatest.String)
		if expected != current {
			return artifacts.ArtifactVersion{}, fmt.Errorf("%w: expectedPrevRef=%q current=%q", artifacts.ErrConflict, expected, current)
		}
	}

	a.PrevRef = strings.TrimSpace(currentLatest.String)

	var parent any
	if a.PrevRef == "" {
		parent = nil
	} else {
		parent = a.PrevRef
	}
	var payload any
	if strings.TrimSpace(a.SHA256) == "" {
		payload = nil
	} else {
		payload = strings.ToLower(strings.TrimSpace(a.SHA256))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO versions(version_id, name, parent_version_id, kind, mime_type, filename, size_bytes, payload_sha256, created_at, tombstone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, a.Ref, a.Name, parent, string(a.Kind), a.MimeType, a.Filename, a.SizeBytes, payload, a.CreatedAt.UTC().Format(time.RFC3339), boolToInt(a.Tombstone)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return artifacts.ArtifactVersion{}, fmt.Errorf("%w: ref already exists", artifacts.ErrConflict)
		}
		return artifacts.ArtifactVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE artifacts
		SET latest_version_id = ?, deleted = 0, updated_at = ?, deleted_at = NULL
		WHERE name = ?;
	`, a.Ref, a.CreatedAt.UTC().Format(time.RFC3339), a.Name); err != nil {
		return artifacts.ArtifactVersion{}, err
	}

	if err := tx.Commit(); err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	return a, nil
}

func (r *ArtifactRepository) Resolve(ctx context.Context, name string) (string, error) {
	var latest sql.NullString
	var deleted int
	err := r.db.QueryRowContext(ctx, `SELECT latest_version_id, deleted FROM artifacts WHERE name = ?;`, name).Scan(&latest, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", artifacts.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if deleted != 0 || strings.TrimSpace(latest.String) == "" {
		return "", artifacts.ErrNotFound
	}
	return latest.String, nil
}

func (r *ArtifactRepository) Get(ctx context.Context, sel artifacts.Selector) (artifacts.ArtifactVersion, []byte, error) {
	ref := strings.TrimSpace(sel.Ref)
	if ref == "" {
		var latest sql.NullString
		var deleted int
		err := r.db.QueryRowContext(ctx, `SELECT latest_version_id, deleted FROM artifacts WHERE name = ?;`, sel.Name).Scan(&latest, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return artifacts.ArtifactVersion{}, nil, artifacts.ErrNotFound
		}
		if err != nil {
			return artifacts.ArtifactVersion{}, nil, err
		}
		if deleted != 0 || strings.TrimSpace(latest.String) == "" {
			return artifacts.ArtifactVersion{}, nil, artifacts.ErrNotFound
		}
		ref = latest.String
	}

	a, err := r.getVersionMeta(ctx, ref)
	if err != nil {
		return artifacts.ArtifactVersion{}, nil, err
	}
	if a.Tombstone || strings.TrimSpace(a.SHA256) == "" {
		return a, []byte{}, nil
	}
	b, err := r.blobs.Get(a.SHA256)
	if err != nil {
		if os.IsNotExist(err) {
			return artifacts.ArtifactVersion{}, nil, artifacts.ErrNotFound
		}
		return artifacts.ArtifactVersion{}, nil, err
	}
	return a, b, nil
}

func (r *ArtifactRepository) List(ctx context.Context, prefix string, limit int) ([]artifacts.ArtifactVersion, error) {
	if limit <= 0 {
		limit = 200
	}
	pattern := "%"
	if prefix != "" {
		pattern = escapeLikePrefix(prefix) + "%"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.version_id, v.name, v.parent_version_id, v.kind, v.mime_type, v.filename, v.size_bytes, v.payload_sha256, v.created_at, v.tombstone
		FROM artifacts a
		JOIN versions v ON v.version_id = a.latest_version_id
		WHERE a.deleted = 0 AND a.name LIKE ? ESCAPE '\'
		ORDER BY a.name ASC
		LIMIT ?;
	`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer closeRowsIgnore(rows)

	out := make([]artifacts.ArtifactVersion, 0)
	for rows.Next() {
		a, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ArtifactRepository) ListVersions(ctx context.Context, name string, limit int) ([]artifacts.ArtifactVersion, error) {
	if limit <= 0 {
		limit = 200
	}
	var latest sql.NullString
	var deleted int
	err := r.db.QueryRowContext(ctx, `SELECT latest_version_id, deleted FROM artifacts WHERE name = ?;`, name).Scan(&latest, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, artifacts.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if deleted != 0 || strings.TrimSpace(latest.String) == "" {
		return nil, artifacts.ErrNotFound
	}

	ref := strings.TrimSpace(latest.String)
	seen := map[string]struct{}{}
	out := make([]artifacts.ArtifactVersion, 0, limit)
	for ref != "" && len(out) < limit {
		if _, exists := seen[ref]; exists {
			break
		}
		seen[ref] = struct{}{}

		meta, err := r.getVersionMeta(ctx, ref)
		if err != nil {
			if errors.Is(err, artifacts.ErrNotFound) {
				break
			}
			return nil, err
		}
		out = append(out, meta)
		ref = strings.TrimSpace(meta.PrevRef)
	}

	if len(out) == 0 {
		return nil, artifacts.ErrNotFound
	}
	return out, nil
}

func (r *ArtifactRepository) Delete(ctx context.Context, sel artifacts.Selector) (artifacts.ArtifactVersion, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		out, err := r.deleteOnce(ctx, sel)
		if err == nil {
			return out, nil
		}
		if !isRetryableBusyErr(err) {
			return artifacts.ArtifactVersion{}, err
		}
		lastErr = err
		time.Sleep(time.Duration(10+attempt*20) * time.Millisecond)
	}
	if lastErr != nil {
		return artifacts.ArtifactVersion{}, lastErr
	}
	return artifacts.ArtifactVersion{}, fmt.Errorf("%w: delete failed", artifacts.ErrInternal)
}

func (r *ArtifactRepository) deleteOnce(ctx context.Context, sel artifacts.Selector) (artifacts.ArtifactVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	defer rollbackIgnore(tx)

	name := strings.TrimSpace(sel.Name)
	ref := strings.TrimSpace(sel.Ref)
	if name == "" {
		if ref == "" {
			return artifacts.ArtifactVersion{}, artifacts.ErrRefOrName
		}
		if err := tx.QueryRowContext(ctx, `SELECT name FROM versions WHERE version_id = ?;`, ref).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
			}
			return artifacts.ArtifactVersion{}, err
		}
	}

	var currentLatest sql.NullString
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT latest_version_id, deleted FROM artifacts WHERE name = ?;`, name).Scan(&currentLatest, &deleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
		}
		return artifacts.ArtifactVersion{}, err
	}
	if deleted != 0 || strings.TrimSpace(currentLatest.String) == "" {
		return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
	}
	if ref != "" && strings.TrimSpace(currentLatest.String) != ref {
		return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
	}

	kind := artifacts.ArtifactKindText
	mimeType := "application/octet-stream"
	if err := tx.QueryRowContext(ctx, `SELECT kind, mime_type FROM versions WHERE version_id = ?;`, currentLatest.String).Scan(&kind, &mimeType); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return artifacts.ArtifactVersion{}, err
		}
	}

	tombRef, err := newVersionRef()
	if err != nil {
		return artifacts.ArtifactVersion{}, fmt.Errorf("%w: generate tombstone ref: %v", artifacts.ErrInternal, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO versions(version_id, name, parent_version_id, kind, mime_type, filename, size_bytes, payload_sha256, created_at, tombstone)
		VALUES (?, ?, ?, ?, ?, '', 0, NULL, ?, 1);
	`, tombRef, name, currentLatest.String, string(kind), mimeType, now.Format(time.RFC3339)); err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE artifacts
		SET latest_version_id = ?, deleted = 1, updated_at = ?, deleted_at = ?
		WHERE name = ?;
	`, tombRef, now.Format(time.RFC3339), now.Format(time.RFC3339), name); err != nil {
		return artifacts.ArtifactVersion{}, err
	}

	if err := tx.Commit(); err != nil {
		return artifacts.ArtifactVersion{}, err
	}

	return artifacts.ArtifactVersion{
		Ref:       tombRef,
		Name:      name,
		Kind:      kind,
		MimeType:  mimeType,
		SizeBytes: 0,
		CreatedAt: now,
		PrevRef:   currentLatest.String,
		Tombstone: true,
	}, nil
}

func (r *ArtifactRepository) getVersionMeta(ctx context.Context, ref string) (artifacts.ArtifactVersion, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT version_id, name, parent_version_id, kind, mime_type, filename, size_bytes, payload_sha256, created_at, tombstone
		FROM versions
		WHERE version_id = ?;
	`, ref)
	a, err := scanVersionRow(row)
	if err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	return a, nil
}

func scanVersion(rows *sql.Rows) (artifacts.ArtifactVersion, error) {
	var (
		versionID string
		name      string
		parent    sql.NullString
		kind      string
		mimeType  string
		filename  string
		sizeBytes int64
		sha       sql.NullString
		createdAt string
		tomb      int
	)
	if err := rows.Scan(&versionID, &name, &parent, &kind, &mimeType, &filename, &sizeBytes, &sha, &createdAt, &tomb); err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	return buildVersion(versionID, name, parent, kind, mimeType, filename, sizeBytes, sha, createdAt, tomb)
}

func scanVersionRow(row *sql.Row) (artifacts.ArtifactVersion, error) {
	var (
		versionID string
		name      string
		parent    sql.NullString
		kind      string
		mimeType  string
		filename  string
		sizeBytes int64
		sha       sql.NullString
		createdAt string
		tomb      int
	)
	if err := row.Scan(&versionID, &name, &parent, &kind, &mimeType, &filename, &sizeBytes, &sha, &createdAt, &tomb); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return artifacts.ArtifactVersion{}, artifacts.ErrNotFound
		}
		return artifacts.ArtifactVersion{}, err
	}
	return buildVersion(versionID, name, parent, kind, mimeType, filename, sizeBytes, sha, createdAt, tomb)
}

func buildVersion(versionID, name string, parent sql.NullString, kind, mimeType, filename string, sizeBytes int64, sha sql.NullString, createdAt string, tomb int) (artifacts.ArtifactVersion, error) {
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return artifacts.ArtifactVersion{}, err
	}
	return artifacts.ArtifactVersion{
		Ref:       versionID,
		Name:      name,
		PrevRef:   strings.TrimSpace(parent.String),
		Kind:      artifacts.ArtifactKind(kind),
		MimeType:  mimeType,
		Filename:  filename,
		SizeBytes: sizeBytes,
		SHA256:    strings.TrimSpace(sha.String),
		CreatedAt: parsed,
		Tombstone: tomb != 0,
	}, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func escapeLikePrefix(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch r {
		case '\\', '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isRetryableBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy")
}

func newVersionRef() (string, error) {
	ts := time.Now().UTC().Truncate(time.Second).Format("20060102T150405Z")
	rnd := make([]byte, 8)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	return ts + "-" + hex.EncodeToString(rnd), nil
}
