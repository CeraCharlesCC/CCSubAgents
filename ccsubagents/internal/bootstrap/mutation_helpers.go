package bootstrap

import "path/filepath"

type mutationTracker struct {
	rollback    *installRollback
	createdDirs []string
}

func newMutationTracker(rollback *installRollback) *mutationTracker {
	return &mutationTracker{rollback: rollback}
}

func (m *mutationTracker) ensureDir(path string) error {
	created, err := ensureDirTracked(path)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	clean := filepath.Clean(path)
	m.createdDirs = append(m.createdDirs, clean)
	m.rollback.trackCreatedDir(clean)
	return nil
}

func (m *mutationTracker) ensureParentDir(path string) error {
	return m.ensureDir(filepath.Dir(path))
}

func (m *mutationTracker) snapshotFile(path string) error {
	return m.rollback.captureFile(path)
}

func (m *mutationTracker) createdDirectories() []string {
	return uniqueSorted(m.createdDirs)
}
