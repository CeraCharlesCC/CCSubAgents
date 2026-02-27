package txn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
)

func TestEngine_RollsBackAppliedActionsOnFailure(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	blobDir := filepath.Join(t.TempDir(), "blob")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	engine := Engine{StateDir: stateDir, BlobDir: blobDir}
	err := engine.Execute(context.Background(), Plan{
		ScopeID: "global",
		Command: "install",
		Steps: []Step{
			{
				ID: "write-target",
				Apply: func(_ context.Context, s *Session) error {
					rb := s.NewRollback("write-target")
					m := files.NewMutationTracker(rb, files.DefaultStateDirPerm)
					if err := m.SnapshotFile(target); err != nil {
						return err
					}
					return os.WriteFile(target, []byte("new"), 0o600)
				},
			},
			{
				ID:        "verify",
				DependsOn: []string{"write-target"},
				Verify: func(_ context.Context, _ *Session) error {
					return errors.New("verify failed")
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	b, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(b) != "old" {
		t.Fatalf("rollback did not restore file: %q", string(b))
	}
}

func TestRecover_RollsBackStaleJournal(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	blobDir := filepath.Join(t.TempDir(), "blob")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	session, err := Begin(stateDir, blobDir, "global", "install", []string{"write"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rb := session.NewRollback("write")
	m := files.NewMutationTracker(rb, files.DefaultStateDirPerm)
	if err := m.SnapshotFile(target); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(target, []byte("after"), 0o600); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if err := session.MarkApplied("write"); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	session.Close()

	if err := Recover(stateDir, "global"); err != nil {
		t.Fatalf("recover: %v", err)
	}
	b, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(b) != "before" {
		t.Fatalf("expected recovered file content, got %q", string(b))
	}
}
