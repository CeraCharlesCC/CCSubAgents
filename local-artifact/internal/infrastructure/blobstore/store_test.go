package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func digestFor(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestStorePutIdempotent(t *testing.T) {
	s := New(t.TempDir())
	data := []byte("same content")
	digest := digestFor(data)

	if err := s.Put(digest, data); err != nil {
		t.Fatalf("first put failed: %v", err)
	}
	if err := s.Put(digest, data); err != nil {
		t.Fatalf("second put failed: %v", err)
	}

	got, err := s.Get(digest)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("unexpected blob contents: got=%q want=%q", string(got), string(data))
	}
}

func TestStoreDedupesSingleFilePath(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	data := []byte("shared payload")
	digest := digestFor(data)

	if err := s.Put(digest, data); err != nil {
		t.Fatalf("put one failed: %v", err)
	}
	if err := s.Put(digest, data); err != nil {
		t.Fatalf("put two failed: %v", err)
	}

	path := s.Path(digest)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected blob path %q: %v", path, err)
	}

	entries, err := os.ReadDir(filepath.Join(root, digest[:2]))
	if err != nil {
		t.Fatalf("read digest shard dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != digest {
		t.Fatalf("expected exactly one blob file %q, got: %+v", digest, entries)
	}
}
