package installer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathslib "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func TestStopDaemonBeforeRemoval_IgnoresMetadataOnlyRegistryIssues(t *testing.T) {
	stateDir := t.TempDir()
	roleDir := filepath.Join(stateDir, "daemon", "processes", "web")
	if err := os.MkdirAll(roleDir, stateDirPerm); err != nil {
		t.Fatalf("create role dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(roleDir, "abc.pid"), []byte("invalid\n"), stateFilePerm); err != nil {
		t.Fatalf("write invalid pid file: %v", err)
	}

	var status bytes.Buffer
	runner := &Runner{
		homeDir: func() (string, error) { return t.TempDir(), nil },
		getenv: func(key string) string {
			if key == pathslib.EnvStateDir {
				return stateDir
			}
			return ""
		},
		statusOut: &status,
	}

	if err := runner.stopDaemonBeforeRemoval(context.Background()); err != nil {
		t.Fatalf("expected metadata-only registry issues to be ignored, got %v", err)
	}
	if !strings.Contains(status.String(), "Ignoring stale daemon process metadata") {
		t.Fatalf("expected warning output, got %q", status.String())
	}
}

func TestStopDaemonBeforeRemoval_FailsOnNonMetadataRegistryErrors(t *testing.T) {
	stateDir := t.TempDir()
	rolePath := filepath.Join(stateDir, "daemon", "processes", "web")
	if err := os.MkdirAll(filepath.Dir(rolePath), stateDirPerm); err != nil {
		t.Fatalf("create processes parent dir: %v", err)
	}
	if err := os.WriteFile(rolePath, []byte("not-a-directory\n"), stateFilePerm); err != nil {
		t.Fatalf("write blocking role file: %v", err)
	}

	runner := &Runner{
		homeDir: func() (string, error) { return t.TempDir(), nil },
		getenv: func(key string) string {
			if key == pathslib.EnvStateDir {
				return stateDir
			}
			return ""
		},
	}

	err := runner.stopDaemonBeforeRemoval(context.Background())
	if err == nil {
		t.Fatalf("expected non-metadata registry error")
	}
	if !strings.Contains(err.Error(), "stop registered daemon processes") {
		t.Fatalf("expected stop registered daemon processes error, got %v", err)
	}
}
