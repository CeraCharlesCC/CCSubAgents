package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func TestRun_ReportsActiveTransactionJournal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	stateDir := paths.Global(home).StateDir
	if err := os.MkdirAll(filepath.Join(stateDir, "tx"), 0o755); err != nil {
		t.Fatalf("mkdir tx: %v", err)
	}
	journal := filepath.Join(stateDir, "tx", "global-active.json")
	if err := os.WriteFile(journal, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	var out bytes.Buffer
	issues, err := Run(context.Background(), Options{
		Home: home,
		CWD:  cwd,
		Out:  &out,
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	})
	if err != nil {
		t.Fatalf("doctor run failed: %v", err)
	}
	if issues == 0 {
		t.Fatalf("expected issues > 0, output=%q", out.String())
	}
	if !strings.Contains(out.String(), "transaction.active=") {
		t.Fatalf("expected active transaction output, got %q", out.String())
	}
}
