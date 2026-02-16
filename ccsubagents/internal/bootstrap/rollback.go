package bootstrap

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

type installRollback struct {
	snapshots   map[string]fileSnapshot
	createdDirs []string
}

func newInstallRollback() *installRollback {
	return &installRollback{snapshots: map[string]fileSnapshot{}}
}

func (r *installRollback) captureFile(path string) error {
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

func (r *installRollback) trackCreatedDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.createdDirs = append(r.createdDirs, filepath.Clean(path))
}

func (r *installRollback) restore() error {
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

	dirs := uniqueSorted(r.createdDirs)
	sort.SliceStable(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if isDirNotEmptyError(err) {
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
