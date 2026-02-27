package txn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
)

const (
	undoTypeFile       = "file"
	undoTypeCreatedDir = "created-dir"
)

type UndoRecord struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Existed    bool   `json:"existed,omitempty"`
	Perm       uint32 `json:"perm,omitempty"`
	BackupFile string `json:"backupFile,omitempty"`
}

type ActionEntry struct {
	ActionID  string       `json:"actionID"`
	Status    string       `json:"status"`
	AppliedAt string       `json:"appliedAt,omitempty"`
	Undo      []UndoRecord `json:"undo,omitempty"`
}

type Journal struct {
	TxID            string        `json:"txID"`
	Scope           string        `json:"scope"`
	Command         string        `json:"command"`
	CreatedAt       string        `json:"createdAt"`
	RollbackBlobDir string        `json:"rollbackBlobDir"`
	Committed       bool          `json:"committed,omitempty"`
	Actions         []ActionEntry `json:"actions"`
}

func journalFilePath(stateDir, scopeID string) string {
	return filepath.Join(stateDir, "tx", scopeID+"-active.json")
}

func writeJournal(path string, j Journal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction journal: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), files.DefaultStateDirPerm); err != nil {
		return fmt.Errorf("create journal dir: %w", err)
	}
	if err := files.WriteFileAtomic(path, data, files.DefaultStateFilePerm); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	return nil
}

func readJournal(path string) (Journal, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Journal{}, err
	}
	var j Journal
	if err := json.Unmarshal(b, &j); err != nil {
		return Journal{}, fmt.Errorf("parse journal %s: %w", path, err)
	}
	return j, nil
}

func newJournal(scopeID, command, rollbackBlobDir string, actionIDs []string) Journal {
	actions := make([]ActionEntry, 0, len(actionIDs))
	for _, id := range actionIDs {
		actions = append(actions, ActionEntry{ActionID: id, Status: "planned"})
	}
	return Journal{
		TxID:            fmt.Sprintf("tx-%d", time.Now().UnixNano()),
		Scope:           scopeID,
		Command:         command,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		RollbackBlobDir: rollbackBlobDir,
		Actions:         actions,
	}
}
