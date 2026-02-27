package blobstore

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) Path(digest string) string {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if len(digest) < 2 {
		return filepath.Join(s.root, digest)
	}
	return filepath.Join(s.root, digest[:2], digest)
}

func (s *Store) Put(digest string, data []byte) error {
	digest, err := normalizeDigest(digest)
	if err != nil {
		return err
	}
	target := s.Path(digest)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	if existing, err := os.ReadFile(target); err == nil {
		if string(existing) != string(data) {
			return fmt.Errorf("digest collision for %s", digest)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".blob-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		if existing, readErr := os.ReadFile(target); readErr == nil {
			if string(existing) == string(data) {
				return nil
			}
		}
		return err
	}
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) Get(digest string) ([]byte, error) {
	digest, err := normalizeDigest(digest)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(s.Path(digest))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func normalizeDigest(digest string) (string, error) {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if len(digest) != 64 {
		return "", fmt.Errorf("invalid sha256 digest length: %q", digest)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("invalid sha256 digest: %w", err)
	}
	return digest, nil
}
