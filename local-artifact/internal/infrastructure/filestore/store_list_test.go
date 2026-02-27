package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestStoreList_FillsLimitAfterSkippingInvalidEntries(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	saveArtifact(t, store, "20260223T120000Z-aaaaaaaaaaaaaaaa", "c/good-1", []byte("c"))
	saveArtifact(t, store, "20260223T120001Z-bbbbbbbbbbbbbbbb", "d/good-2", []byte("d"))

	store.mu.Lock()
	idx, err := store.loadIndexLocked()
	if err != nil {
		store.mu.Unlock()
		t.Fatalf("load index: %v", err)
	}
	idx.Names["a/empty-ref"] = ""
	idx.Names["b/missing-meta"] = "20260223T120002Z-cccccccccccccccc"
	if err := store.saveIndexLocked(idx); err != nil {
		store.mu.Unlock()
		t.Fatalf("save index: %v", err)
	}
	store.mu.Unlock()

	listed, err := store.List(ctx, "", 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 listed artifacts, got %d", len(listed))
	}
	if listed[0].Name != "c/good-1" || listed[1].Name != "d/good-2" {
		t.Fatalf("unexpected listed names: %+v", []string{listed[0].Name, listed[1].Name})
	}
}

func TestStoreList_DropsMissingObjectAndCleansAlias(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	missingRef := "20260223T120100Z-aaaaaaaaaaaaaaaa"
	saveArtifact(t, store, missingRef, "a/missing-object", []byte("missing"))
	saveArtifact(t, store, "20260223T120101Z-bbbbbbbbbbbbbbbb", "b/valid", []byte("valid"))

	if err := os.Remove(filepath.Join(store.root, "objects", missingRef)); err != nil {
		t.Fatalf("remove object: %v", err)
	}

	listed, err := store.List(ctx, "", 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed artifact, got %d", len(listed))
	}
	if listed[0].Name != "b/valid" {
		t.Fatalf("expected only b/valid to remain, got %+v", listed)
	}

	store.mu.Lock()
	idx, err := store.loadIndexLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("load index after list: %v", err)
	}
	if _, ok := idx.Names["a/missing-object"]; ok {
		t.Fatalf("expected missing-object alias to be removed from index")
	}

	_, err = store.Resolve(ctx, "a/missing-object")
	if !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("expected resolve missing-object to be not found, got %v", err)
	}
}

func saveArtifact(t *testing.T, store *Store, ref string, name string, data []byte) {
	t.Helper()
	_, err := store.Save(context.Background(), artifacts.Artifact{
		Ref:       ref,
		Name:      name,
		Kind:      artifacts.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: int64(len(data)),
		SHA256:    "test-sha",
		CreatedAt: time.Now().UTC(),
	}, data, artifacts.SaveOptions{})
	if err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}
