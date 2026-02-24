package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fileSnapshot struct {
	exists bool
	mode   os.FileMode
	data   []byte
}

type Rollback struct {
	snapshots   map[string]fileSnapshot
	createdDirs []string
}

func NewRollback() *Rollback {
	return &Rollback{snapshots: map[string]fileSnapshot{}}
}

func (r *Rollback) CaptureFile(path string) error {
	clean := filepath.Clean(path)
	if _, ok := r.snapshots[clean]; ok {
		return nil
	}
	info, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.snapshots[clean] = fileSnapshot{exists: false}
			return nil
		}
		return fmt.Errorf("stat %s for rollback: %w", clean, err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot snapshot directory for rollback: %s", clean)
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return fmt.Errorf("read %s for rollback: %w", clean, err)
	}
	r.snapshots[clean] = fileSnapshot{
		exists: true,
		mode:   info.Mode().Perm(),
		data:   data,
	}
	return nil
}

func (r *Rollback) TrackCreatedDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.createdDirs = append(r.createdDirs, filepath.Clean(path))
}

func (r *Rollback) Restore() error {
	paths := make([]string, 0, len(r.snapshots))
	for path := range r.snapshots {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var errs []string
	for _, path := range paths {
		snapshot := r.snapshots[path]
		if snapshot.exists {
			if err := os.WriteFile(path, snapshot.data, snapshot.mode); err != nil {
				errs = append(errs, fmt.Sprintf("restore %s: %v", path, err))
			}
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Sprintf("remove %s: %v", path, err))
		}
	}

	dirs := UniqueSorted(r.createdDirs)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if IsDirNotEmptyError(err) {
				continue
			}
			errs = append(errs, fmt.Sprintf("remove created dir %s: %v", dir, err))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type MutationTracker struct {
	rollback    *Rollback
	createdDirs []string
	dirPerm     os.FileMode
}

func NewMutationTracker(rollback *Rollback, dirPerm os.FileMode) *MutationTracker {
	return &MutationTracker{rollback: rollback, dirPerm: dirPerm}
}

func (m *MutationTracker) EnsureDir(path string) error {
	created, err := EnsureDirTracked(path, m.dirPerm)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	clean := filepath.Clean(path)
	m.createdDirs = append(m.createdDirs, clean)
	m.rollback.TrackCreatedDir(clean)
	return nil
}

func (m *MutationTracker) EnsureParentDir(path string) error {
	return m.EnsureDir(filepath.Dir(path))
}

func (m *MutationTracker) SnapshotFile(path string) error {
	return m.rollback.CaptureFile(path)
}

func (m *MutationTracker) CreatedDirectories() []string {
	return UniqueSorted(m.createdDirs)
}

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
