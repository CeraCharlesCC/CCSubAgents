package txn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
)

type Session struct {
	stateDir    string
	scopeID     string
	journalPath string
	journal     Journal

	lockRelease func()
	counter     int
	mu          sync.Mutex
}

func Recover(stateDir, scopeID string) error {
	path := journalFilePath(stateDir, scopeID)
	j, err := readJournal(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if j.Committed {
		_ = os.Remove(path)
		return nil
	}
	if err := rollbackFromJournal(j); err != nil {
		return err
	}
	_ = os.Remove(path)
	_ = os.RemoveAll(j.RollbackBlobDir)
	return nil
}

func Begin(stateDir, blobDir, scopeID, command string, actionIDs []string) (*Session, error) {
	if err := Recover(stateDir, scopeID); err != nil {
		return nil, err
	}
	release, err := acquireLock(stateDir, scopeID)
	if err != nil {
		return nil, err
	}
	txBlobDir := filepath.Join(blobDir, "tx", fmt.Sprintf("%s-%d", scopeID, time.Now().UnixNano()))
	if err := os.MkdirAll(txBlobDir, files.DefaultStateDirPerm); err != nil {
		release()
		return nil, fmt.Errorf("create rollback blob dir: %w", err)
	}
	j := newJournal(scopeID, command, txBlobDir, actionIDs)
	path := journalFilePath(stateDir, scopeID)
	if err := writeJournal(path, j); err != nil {
		release()
		return nil, err
	}
	return &Session{stateDir: stateDir, scopeID: scopeID, journalPath: path, journal: j, lockRelease: release}, nil
}

func (s *Session) MarkApplied(actionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.actionIndex(actionID)
	if idx < 0 {
		return fmt.Errorf("unknown action %q", actionID)
	}
	s.journal.Actions[idx].Status = "applied"
	s.journal.Actions[idx].AppliedAt = time.Now().UTC().Format(time.RFC3339)
	return writeJournal(s.journalPath, s.journal)
}

func (s *Session) NewRollback(actionID string) *files.Rollback {
	return files.NewRollbackWithObserver(&rollbackObserver{session: s, actionID: actionID})
}

func (s *Session) Commit() error {
	s.mu.Lock()
	s.journal.Committed = true
	err := writeJournal(s.journalPath, s.journal)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	_ = os.Remove(s.journalPath)
	_ = os.RemoveAll(s.journal.RollbackBlobDir)
	if s.lockRelease != nil {
		s.lockRelease()
		s.lockRelease = nil
	}
	return nil
}

func (s *Session) Rollback() error {
	s.mu.Lock()
	j := s.journal
	s.mu.Unlock()
	if err := rollbackFromJournal(j); err != nil {
		return err
	}
	_ = os.Remove(s.journalPath)
	_ = os.RemoveAll(j.RollbackBlobDir)
	if s.lockRelease != nil {
		s.lockRelease()
		s.lockRelease = nil
	}
	return nil
}

func (s *Session) Close() {
	if s.lockRelease != nil {
		s.lockRelease()
		s.lockRelease = nil
	}
}

func (s *Session) actionIndex(actionID string) int {
	for i := range s.journal.Actions {
		if s.journal.Actions[i].ActionID == actionID {
			return i
		}
	}
	return -1
}

type rollbackObserver struct {
	session  *Session
	actionID string
}

func (o *rollbackObserver) OnSnapshot(path string, existed bool, mode os.FileMode, data []byte) error {
	s := o.session
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.actionIndex(o.actionID)
	if idx < 0 {
		return fmt.Errorf("unknown action %q", o.actionID)
	}
	record := UndoRecord{Type: undoTypeFile, Path: path, Existed: existed, Perm: uint32(mode.Perm())}
	if existed {
		s.counter++
		backupPath := filepath.Join(s.journal.RollbackBlobDir, fmt.Sprintf("undo-%06d.bin", s.counter))
		if err := os.MkdirAll(filepath.Dir(backupPath), files.DefaultStateDirPerm); err != nil {
			return err
		}
		if err := os.WriteFile(backupPath, data, files.DefaultStateFilePerm); err != nil {
			return err
		}
		record.BackupFile = backupPath
	}
	s.journal.Actions[idx].Undo = append(s.journal.Actions[idx].Undo, record)
	return writeJournal(s.journalPath, s.journal)
}

func (o *rollbackObserver) OnCreatedDir(path string) {
	s := o.session
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.actionIndex(o.actionID)
	if idx < 0 {
		return
	}
	s.journal.Actions[idx].Undo = append(s.journal.Actions[idx].Undo, UndoRecord{Type: undoTypeCreatedDir, Path: path})
	_ = writeJournal(s.journalPath, s.journal)
}

func rollbackFromJournal(j Journal) error {
	var errs []string
	for i := len(j.Actions) - 1; i >= 0; i-- {
		a := j.Actions[i]
		if a.Status != "applied" {
			continue
		}
		for u := len(a.Undo) - 1; u >= 0; u-- {
			rec := a.Undo[u]
			switch rec.Type {
			case undoTypeFile:
				if rec.Existed {
					data, err := os.ReadFile(rec.BackupFile)
					if err != nil {
						errs = append(errs, fmt.Sprintf("read backup %s: %v", rec.BackupFile, err))
						continue
					}
					if err := os.MkdirAll(filepath.Dir(rec.Path), files.DefaultStateDirPerm); err != nil {
						errs = append(errs, fmt.Sprintf("mkdir %s: %v", filepath.Dir(rec.Path), err))
						continue
					}
					if err := os.WriteFile(rec.Path, data, os.FileMode(rec.Perm)); err != nil {
						errs = append(errs, fmt.Sprintf("restore %s: %v", rec.Path, err))
					}
				} else {
					if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
						errs = append(errs, fmt.Sprintf("remove %s: %v", rec.Path, err))
					}
				}
			case undoTypeCreatedDir:
				if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
					if !isDirNotEmpty(err) {
						errs = append(errs, fmt.Sprintf("remove dir %s: %v", rec.Path, err))
					}
				}
			}
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func isDirNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "directory not empty")
}
