package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) ensureDirs() error {
	dirs := []string{
		filepath.Join(s.root, "objects"),
		filepath.Join(s.root, "meta"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Save(ctx context.Context, a domain.Artifact, data []byte) (domain.Artifact, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDirs(); err != nil {
		return domain.Artifact{}, fmt.Errorf("ensure dirs: %w", err)
	}

	idx, err := s.loadIndexLocked()
	if err != nil {
		return domain.Artifact{}, err
	}
	if existingRef := strings.TrimSpace(idx.Names[a.Name]); existingRef != "" && existingRef != a.Ref && strings.TrimSpace(a.PrevRef) == "" {
		a.PrevRef = existingRef
	}

	objPath := filepath.Join(s.root, "objects", a.Ref)
	if err := atomicWriteFile(objPath, data, 0o644); err != nil {
		return domain.Artifact{}, fmt.Errorf("write object: %w", err)
	}

	metaBytes, err := json.Marshal(a)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("marshal meta: %w", err)
	}
	metaPath := filepath.Join(s.root, "meta", a.Ref+".json")
	if err := atomicWriteFile(metaPath, metaBytes, 0o644); err != nil {
		return domain.Artifact{}, fmt.Errorf("write meta: %w", err)
	}

	idx.Names[a.Name] = a.Ref
	idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.saveIndexLocked(idx); err != nil {
		return domain.Artifact{}, err
	}

	return a, nil
}

func (s *Store) Resolve(ctx context.Context, name string) (string, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.loadIndexLocked()
	if err != nil {
		return "", err
	}
	ref, ok := idx.Names[name]
	if !ok || strings.TrimSpace(ref) == "" {
		return "", domain.ErrNotFound
	}

	exists, err := s.artifactExistsLocked(ref)
	if err != nil {
		return "", err
	}
	if !exists {
		delete(idx.Names, name)
		idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveIndexLocked(idx); err != nil {
			return "", err
		}
		return "", domain.ErrNotFound
	}

	return ref, nil
}

func (s *Store) Get(ctx context.Context, sel domain.Selector) (domain.Artifact, []byte, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	ref := strings.TrimSpace(sel.Ref)
	resolvedByName := strings.TrimSpace(sel.Name)
	if ref == "" {
		idx, err := s.loadIndexLocked()
		if err != nil {
			return domain.Artifact{}, nil, err
		}
		r, ok := idx.Names[resolvedByName]
		if !ok || strings.TrimSpace(r) == "" {
			return domain.Artifact{}, nil, domain.ErrNotFound
		}
		ref = r
	}

	metaPath := filepath.Join(s.root, "meta", ref+".json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			if resolvedByName != "" {
				idx, idxErr := s.loadIndexLocked()
				if idxErr == nil {
					delete(idx.Names, resolvedByName)
					idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					_ = s.saveIndexLocked(idx)
				}
			}
			return domain.Artifact{}, nil, domain.ErrNotFound
		}
		return domain.Artifact{}, nil, fmt.Errorf("read meta: %w", err)
	}
	var a domain.Artifact
	if err := json.Unmarshal(metaBytes, &a); err != nil {
		return domain.Artifact{}, nil, fmt.Errorf("unmarshal meta: %w", err)
	}

	objPath := filepath.Join(s.root, "objects", ref)
	data, err := os.ReadFile(objPath)
	if err != nil {
		if os.IsNotExist(err) {
			if resolvedByName != "" {
				idx, idxErr := s.loadIndexLocked()
				if idxErr == nil {
					delete(idx.Names, resolvedByName)
					idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					_ = s.saveIndexLocked(idx)
				}
			}
			return domain.Artifact{}, nil, domain.ErrNotFound
		}
		return domain.Artifact{}, nil, fmt.Errorf("read object: %w", err)
	}
	return a, data, nil
}

func (s *Store) List(ctx context.Context, prefix string, limit int) ([]domain.Artifact, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}

	prefix = strings.TrimSpace(prefix)
	names := make([]string, 0, len(idx.Names))
	for name := range idx.Names {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if limit <= 0 {
		limit = 200
	}
	if len(names) > limit {
		names = names[:limit]
	}

	out := make([]domain.Artifact, 0, len(names))
	dirtyIndex := false
	for _, name := range names {
		ref := idx.Names[name]
		if strings.TrimSpace(ref) == "" {
			delete(idx.Names, name)
			dirtyIndex = true
			continue
		}
		metaPath := filepath.Join(s.root, "meta", ref+".json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				delete(idx.Names, name)
				dirtyIndex = true
			}
			continue
		}
		var a domain.Artifact
		if err := json.Unmarshal(metaBytes, &a); err != nil {
			delete(idx.Names, name)
			dirtyIndex = true
			continue
		}
		if strings.TrimSpace(a.Name) == "" {
			a.Name = name
		}
		out = append(out, a)
	}
	if dirtyIndex {
		idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveIndexLocked(idx); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func (s *Store) Delete(ctx context.Context, sel domain.Selector) (domain.Artifact, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.loadIndexLocked()
	if err != nil {
		return domain.Artifact{}, err
	}

	ref := strings.TrimSpace(sel.Ref)
	namesToDelete := make([]string, 0, 1)

	if ref == "" {
		name := strings.TrimSpace(sel.Name)
		r, ok := idx.Names[name]
		if !ok || strings.TrimSpace(r) == "" {
			return domain.Artifact{}, domain.ErrNotFound
		}
		ref = r
		namesToDelete = append(namesToDelete, name)
	} else {
		for name, curRef := range idx.Names {
			if strings.TrimSpace(curRef) == ref {
				namesToDelete = append(namesToDelete, name)
			}
		}
		sort.Strings(namesToDelete)
	}

	metaPath := filepath.Join(s.root, "meta", ref+".json")
	objPath := filepath.Join(s.root, "objects", ref)

	var out domain.Artifact
	metaBytes, metaErr := os.ReadFile(metaPath)
	if metaErr == nil {
		if err := json.Unmarshal(metaBytes, &out); err != nil {
			return domain.Artifact{}, fmt.Errorf("unmarshal meta: %w", err)
		}
	} else if !os.IsNotExist(metaErr) {
		return domain.Artifact{}, fmt.Errorf("read meta: %w", metaErr)
	}

	objRemoved, metaRemoved := false, false
	if err := os.Remove(objPath); err == nil {
		objRemoved = true
	} else if !os.IsNotExist(err) {
		return domain.Artifact{}, fmt.Errorf("delete object: %w", err)
	}
	if err := os.Remove(metaPath); err == nil {
		metaRemoved = true
	} else if !os.IsNotExist(err) {
		return domain.Artifact{}, fmt.Errorf("delete meta: %w", err)
	}

	for _, name := range namesToDelete {
		delete(idx.Names, name)
	}
	if len(namesToDelete) > 0 {
		idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveIndexLocked(idx); err != nil {
			return domain.Artifact{}, err
		}
	}

	if len(namesToDelete) == 0 && !objRemoved && !metaRemoved && errors.Is(metaErr, os.ErrNotExist) {
		return domain.Artifact{}, domain.ErrNotFound
	}

	if strings.TrimSpace(out.Ref) == "" {
		out.Ref = ref
	}
	if strings.TrimSpace(out.Name) == "" && len(namesToDelete) > 0 {
		out.Name = namesToDelete[0]
	}
	return out, nil
}

func (s *Store) artifactExistsLocked(ref string) (bool, error) {
	metaPath := filepath.Join(s.root, "meta", ref+".json")
	if _, err := os.Stat(metaPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat meta: %w", err)
	}

	objPath := filepath.Join(s.root, "objects", ref)
	if _, err := os.Stat(objPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat object: %w", err)
	}
	return true, nil
}

// --- Index helpers ---

type indexFile struct {
	Version   int               `json:"version"`
	UpdatedAt string            `json:"updatedAt"`
	Names     map[string]string `json:"names"`
}

func (s *Store) indexPath() string {
	return filepath.Join(s.root, "names.json")
}

func (s *Store) loadIndexLocked() (indexFile, error) {
	if err := s.ensureDirs(); err != nil {
		return indexFile{}, fmt.Errorf("ensure dirs: %w", err)
	}

	b, err := os.ReadFile(s.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return indexFile{Version: 1, Names: map[string]string{}}, nil
		}
		return indexFile{}, fmt.Errorf("read index: %w", err)
	}
	var idx indexFile
	if err := json.Unmarshal(b, &idx); err != nil {
		return indexFile{}, fmt.Errorf("unmarshal index: %w", err)
	}
	if idx.Names == nil {
		idx.Names = map[string]string{}
	}
	if idx.Version == 0 {
		idx.Version = 1
	}
	return idx, nil
}

func (s *Store) saveIndexLocked(idx indexFile) error {
	b, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	if err := atomicWriteFile(s.indexPath(), b, 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Create temp file in same dir so os.Rename is atomic.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
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
	// Best-effort fsync directory to reduce corruption risk (linux/mac). Ignore errors.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return os.Rename(tmpName, path)
}
