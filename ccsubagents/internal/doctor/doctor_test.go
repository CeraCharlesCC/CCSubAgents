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

func TestRun_UsesDaemonStateDirForDaemonDiagnostics(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	workspaceState := filepath.Join(cwd, "ccsubagents")
	if err := os.MkdirAll(workspaceState, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	daemonState := paths.ResolveDaemonStateDir(home, func(string) string { return "" })
	wantTokenPath := filepath.Join(daemonState, "daemon", "daemon.token")

	var out bytes.Buffer
	_, err := Run(context.Background(), Options{
		Home: home,
		CWD:  cwd,
		Out:  &out,
		Getenv: func(string) string {
			return ""
		},
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	})
	if err != nil {
		t.Fatalf("doctor run failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "paths.state="+workspaceState+" ("+string(paths.SourceWorkspace)+")") {
		t.Fatalf("expected workspace state in paths output, got %q", got)
	}
	if !strings.Contains(got, "daemon.state="+daemonState) {
		t.Fatalf("expected daemon state output, got %q", got)
	}
	if !strings.Contains(got, "daemon.token=missing (stat "+wantTokenPath+":") {
		t.Fatalf("expected daemon token path under daemon state dir, got %q", got)
	}
	if strings.Contains(got, filepath.Join(workspaceState, "daemon", "daemon.token")) {
		t.Fatalf("daemon token unexpectedly resolved to workspace state dir, got %q", got)
	}
}

func TestRun_DaemonStateDirEnvOverrideAffectsDaemonDiagnostics(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	override := filepath.Join(t.TempDir(), "daemon-state")
	wantTokenPath := filepath.Join(override, "daemon", "daemon.token")

	var out bytes.Buffer
	_, err := Run(context.Background(), Options{
		Home: home,
		CWD:  cwd,
		Out:  &out,
		Getenv: func(key string) string {
			if key == "LOCAL_ARTIFACT_STATE_DIR" {
				return override
			}
			return ""
		},
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	})
	if err != nil {
		t.Fatalf("doctor run failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "daemon.state="+override) {
		t.Fatalf("expected overridden daemon state output, got %q", got)
	}
	if !strings.Contains(got, "daemon.token=missing (stat "+wantTokenPath+":") {
		t.Fatalf("expected daemon token path under overridden daemon state dir, got %q", got)
	}
}
