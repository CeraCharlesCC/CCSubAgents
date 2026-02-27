package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/filestore"
	artsqlite "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/sqlite"
)

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestMigrateWorkspaceIfNeeded_ImportsLegacyFilestoreChains(t *testing.T) {
	root := t.TempDir()
	legacySvc := artifacts.NewService(filestore.New(root))
	ctx := context.Background()

	first, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/task-legacy", Text: "first"})
	if err != nil {
		t.Fatalf("seed first legacy artifact: %v", err)
	}
	second, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/task-legacy", Text: "second"})
	if err != nil {
		t.Fatalf("seed second legacy artifact: %v", err)
	}

	report, err := MigrateWorkspaceIfNeeded(ctx, root)
	if err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	if !report.Migrated {
		t.Fatalf("expected migration to run")
	}
	if report.ImportedVersions < 2 {
		t.Fatalf("expected at least 2 imported versions, got %d", report.ImportedVersions)
	}

	if _, err := os.Stat(filepath.Join(root, "legacy", "names.json")); err != nil {
		t.Fatalf("expected legacy names.json archived: %v", err)
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	resolved, err := repo.Resolve(ctx, "plan/task-legacy")
	if err != nil {
		t.Fatalf("resolve migrated name: %v", err)
	}
	if resolved != second.Ref {
		t.Fatalf("expected latest ref=%q got=%q", second.Ref, resolved)
	}

	oldMeta, oldData, err := repo.Get(ctx, artifacts.Selector{Ref: first.Ref})
	if err != nil {
		t.Fatalf("get first legacy version by ref: %v", err)
	}
	if oldMeta.Ref != first.Ref || string(oldData) != "first" {
		t.Fatalf("unexpected first version payload: meta=%+v data=%q", oldMeta, string(oldData))
	}
}

func TestMigrateWorkspaceIfNeeded_FailedMigrationCanBeRetried(t *testing.T) {
	root := t.TempDir()
	legacySvc := artifacts.NewService(filestore.New(root))
	ctx := context.Background()

	first, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/task-retry", Text: "first"})
	if err != nil {
		t.Fatalf("seed first legacy artifact: %v", err)
	}
	second, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/task-retry", Text: "second"})
	if err != nil {
		t.Fatalf("seed second legacy artifact: %v", err)
	}

	secondMetaPath := filepath.Join(root, "meta", second.Ref+".json")
	originalMeta, err := os.ReadFile(secondMetaPath)
	if err != nil {
		t.Fatalf("read second legacy meta: %v", err)
	}

	var malformed map[string]any
	if err := json.Unmarshal(originalMeta, &malformed); err != nil {
		t.Fatalf("unmarshal second legacy meta: %v", err)
	}
	malformed["ref"] = first.Ref
	badMeta, err := json.Marshal(malformed)
	if err != nil {
		t.Fatalf("marshal malformed meta: %v", err)
	}
	if err := os.WriteFile(secondMetaPath, badMeta, 0o644); err != nil {
		t.Fatalf("write malformed meta: %v", err)
	}

	if _, err := MigrateWorkspaceIfNeeded(ctx, root); err == nil {
		t.Fatal("expected first migration attempt to fail")
	}
	if _, err := os.Stat(filepath.Join(root, "meta.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("expected no final meta.sqlite after failed migration attempt, got err=%v", err)
	}

	if err := os.WriteFile(secondMetaPath, originalMeta, 0o644); err != nil {
		t.Fatalf("restore second legacy meta: %v", err)
	}

	report, err := MigrateWorkspaceIfNeeded(ctx, root)
	if err != nil {
		t.Fatalf("retry migration should succeed: %v", err)
	}
	if !report.Migrated {
		t.Fatalf("expected retry migration to run")
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	resolved, err := repo.Resolve(ctx, "plan/task-retry")
	if err != nil {
		t.Fatalf("resolve migrated retry artifact: %v", err)
	}
	if resolved != second.Ref {
		t.Fatalf("expected retry migration latest ref=%q got=%q", second.Ref, resolved)
	}
}

func TestMigrateWorkspaceIfNeeded_RecoversSplitArchiveStateWithNamesInLegacy(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	legacySvc := artifacts.NewService(filestore.New(root))

	first, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/split-state", Text: "first"})
	if err != nil {
		t.Fatalf("seed first legacy artifact: %v", err)
	}
	second, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/split-state", Text: "second"})
	if err != nil {
		t.Fatalf("seed second legacy artifact: %v", err)
	}

	legacyDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, "names.json"), filepath.Join(legacyDir, "names.json")); err != nil {
		t.Fatalf("move names.json to legacy: %v", err)
	}
	if err := os.Rename(filepath.Join(root, "meta"), filepath.Join(legacyDir, "meta")); err != nil {
		t.Fatalf("move meta dir to legacy: %v", err)
	}

	report, err := MigrateWorkspaceIfNeeded(ctx, root)
	if err != nil {
		t.Fatalf("migrate split-state workspace: %v", err)
	}
	if !report.Migrated || report.ImportedVersions < 2 {
		t.Fatalf("expected split-state migration to import versions, got %+v", report)
	}

	if _, err := os.Stat(filepath.Join(root, "legacy", "objects", second.Ref)); err != nil {
		t.Fatalf("expected root objects to be archived on retry: %v", err)
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	resolved, err := repo.Resolve(ctx, "plan/split-state")
	if err != nil {
		t.Fatalf("resolve migrated split-state name: %v", err)
	}
	if resolved != second.Ref {
		t.Fatalf("expected latest ref=%q got=%q", second.Ref, resolved)
	}

	oldMeta, oldData, err := repo.Get(ctx, artifacts.Selector{Ref: first.Ref})
	if err != nil {
		t.Fatalf("get first split-state version by ref: %v", err)
	}
	if oldMeta.Ref != first.Ref || string(oldData) != "first" {
		t.Fatalf("unexpected split-state first payload: meta=%+v data=%q", oldMeta, string(oldData))
	}
}

func TestMigrateWorkspaceIfNeeded_RecoversSplitArchiveStateWithoutNamesIndex(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	legacySvc := artifacts.NewService(filestore.New(root))

	first, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/split-no-names", Text: "first"})
	if err != nil {
		t.Fatalf("seed first legacy artifact: %v", err)
	}
	second, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/split-no-names", Text: "second"})
	if err != nil {
		t.Fatalf("seed second legacy artifact: %v", err)
	}

	legacyDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, "meta"), filepath.Join(legacyDir, "meta")); err != nil {
		t.Fatalf("move meta dir to legacy: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "names.json")); err != nil {
		t.Fatalf("remove names.json: %v", err)
	}

	report, err := MigrateWorkspaceIfNeeded(ctx, root)
	if err != nil {
		t.Fatalf("migrate split-state workspace without names index: %v", err)
	}
	if !report.Migrated || report.ImportedVersions < 2 {
		t.Fatalf("expected split-state migration without names index to import versions, got %+v", report)
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	versions, err := repo.ListVersions(ctx, "plan/split-no-names", 10)
	if err != nil {
		t.Fatalf("list migrated versions without names index: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 migrated versions, got %d", len(versions))
	}
	if versions[0].Ref != second.Ref || versions[1].Ref != first.Ref {
		t.Fatalf("unexpected split-state version order without names index: %+v", versions)
	}
}

func TestMigrateWorkspaceIfNeeded_ReconstructsChainsWhenNamesIndexMissing(t *testing.T) {
	root := t.TempDir()
	legacySvc := artifacts.NewService(filestore.New(root))
	ctx := context.Background()

	first, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/reconstruct", Text: "old"})
	if err != nil {
		t.Fatalf("seed first legacy artifact: %v", err)
	}
	second, err := legacySvc.SaveText(ctx, artifacts.SaveTextInput{Name: "plan/reconstruct", Text: "new"})
	if err != nil {
		t.Fatalf("seed second legacy artifact: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "names.json")); err != nil {
		t.Fatalf("remove names.json: %v", err)
	}

	report, err := MigrateWorkspaceIfNeeded(ctx, root)
	if err != nil {
		t.Fatalf("migrate from reconstructed chains: %v", err)
	}
	if !report.Migrated {
		t.Fatalf("expected migration to run")
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	versions, err := repo.ListVersions(ctx, "plan/reconstruct", 10)
	if err != nil {
		t.Fatalf("list reconstructed versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 reconstructed versions, got %d", len(versions))
	}
	if versions[0].Ref != second.Ref || versions[1].Ref != first.Ref {
		t.Fatalf("unexpected reconstructed version order: %+v", versions)
	}
}

func TestMigrateWorkspaceIfNeeded_NoImportableChainsFailsAndDoesNotArchive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "meta"), 0o755); err != nil {
		t.Fatalf("mkdir legacy meta: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o755); err != nil {
		t.Fatalf("mkdir legacy objects: %v", err)
	}

	ref := "20260216T120000Z-aaaaaaaaaaaaaaaa"
	payload := []byte("orphan")
	meta := artifacts.ArtifactVersion{
		Ref:       ref,
		Name:      "",
		Kind:      artifacts.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: int64(len(payload)),
		SHA256:    digest(payload),
		CreatedAt: time.Now().UTC(),
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal orphan meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meta", ref+".json"), metaBytes, 0o644); err != nil {
		t.Fatalf("write orphan meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "objects", ref), payload, 0o644); err != nil {
		t.Fatalf("write orphan object: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "names.json"), []byte("{this-is-not-json}"), 0o644); err != nil {
		t.Fatalf("write corrupt names.json: %v", err)
	}

	_, err = MigrateWorkspaceIfNeeded(context.Background(), root)
	if !errors.Is(err, ErrNoImportableLegacyChains) {
		t.Fatalf("expected ErrNoImportableLegacyChains, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(root, "meta", ref+".json")); statErr != nil {
		t.Fatalf("expected legacy meta to remain in place, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "legacy", "meta", ref+".json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected legacy archive to be absent, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "meta.sqlite")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no final meta.sqlite on failed migration, stat err=%v", statErr)
	}
}

func TestMigrateWorkspaceIfNeeded_PreservesPrevRefTopologyWithEqualTimestamps(t *testing.T) {
	root := t.TempDir()
	store := filestore.New(root)
	ctx := context.Background()
	createdAt := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)

	refs := []string{
		"20260216T120000Z-ffffffffffffffff",
		"20260216T120000Z-1111111111111111",
		"20260216T120000Z-0000000000000000",
	}
	payloads := []string{"v1", "v2", "v3"}

	for i, ref := range refs {
		data := []byte(payloads[i])
		_, err := store.Save(ctx, artifacts.ArtifactVersion{
			Ref:       ref,
			Name:      "plan/topology-equal-ts",
			Kind:      artifacts.ArtifactKindText,
			MimeType:  "text/plain; charset=utf-8",
			SizeBytes: int64(len(data)),
			SHA256:    digest(data),
			CreatedAt: createdAt,
		}, data, artifacts.SaveOptions{})
		if err != nil {
			t.Fatalf("seed legacy ref %s: %v", ref, err)
		}
	}

	if _, err := MigrateWorkspaceIfNeeded(ctx, root); err != nil {
		t.Fatalf("migrate equal timestamp chain: %v", err)
	}

	repo, err := artsqlite.NewArtifactRepository(root)
	if err != nil {
		t.Fatalf("open sqlite repo: %v", err)
	}
	versions, err := repo.ListVersions(ctx, "plan/topology-equal-ts", 10)
	if err != nil {
		t.Fatalf("list migrated versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0].Ref != refs[2] || versions[1].Ref != refs[1] || versions[2].Ref != refs[0] {
		t.Fatalf("unexpected migrated version order: %+v", versions)
	}
	if versions[0].PrevRef != refs[1] || versions[1].PrevRef != refs[0] || versions[2].PrevRef != "" {
		t.Fatalf("unexpected migrated prevRef topology: %+v", versions)
	}
}
