package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_UsesGlobalByDefault(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	resolved := Resolve(home, cwd, func(string) string { return "" })

	if resolved.ConfigDir.Source != SourceGlobal {
		t.Fatalf("config source mismatch: got=%q want=%q", resolved.ConfigDir.Source, SourceGlobal)
	}
	if resolved.StateDir.Source != SourceGlobal {
		t.Fatalf("state source mismatch: got=%q want=%q", resolved.StateDir.Source, SourceGlobal)
	}
	if resolved.ConfigDir.Value == resolved.StateDir.Value {
		t.Fatalf("expected config/state split, got config=%q state=%q", resolved.ConfigDir.Value, resolved.StateDir.Value)
	}
	if resolved.ConfigDir.Value != filepath.Join(home, ".local", "share", "ccsubagents", "config") {
		t.Fatalf("config path mismatch: %q", resolved.ConfigDir.Value)
	}
	if resolved.StateDir.Value != filepath.Join(home, ".local", "share", "ccsubagents") {
		t.Fatalf("state path mismatch: %q", resolved.StateDir.Value)
	}
}

func TestResolve_UsesWorkspaceWhenWorkspaceExists(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "ccsubagents"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	resolved := Resolve(home, cwd, func(string) string { return "" })

	if resolved.ConfigDir.Source != SourceWorkspace {
		t.Fatalf("config source mismatch: got=%q want=%q", resolved.ConfigDir.Source, SourceWorkspace)
	}
	if resolved.ConfigDir.Value != filepath.Join(cwd, "ccsubagents") {
		t.Fatalf("workspace config mismatch: %q", resolved.ConfigDir.Value)
	}
	if resolved.StateDir.Value != filepath.Join(cwd, "ccsubagents") {
		t.Fatalf("workspace state mismatch: %q", resolved.StateDir.Value)
	}
}

func TestResolve_EnvOverridesWorkspaceAndGlobal(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "ccsubagents"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	values := map[string]string{
		EnvConfigDir: filepath.Join(t.TempDir(), "cfg"),
		EnvStateDir:  filepath.Join(t.TempDir(), "st"),
		EnvLogDir:    filepath.Join(t.TempDir(), "lg"),
		EnvBlobDir:   filepath.Join(t.TempDir(), "bb"),
	}
	resolved := Resolve(home, cwd, func(key string) string { return values[key] })

	if resolved.ConfigDir.Source != SourceEnv || resolved.ConfigDir.Value != values[EnvConfigDir] {
		t.Fatalf("config override mismatch: %+v", resolved.ConfigDir)
	}
	if resolved.StateDir.Source != SourceEnv || resolved.StateDir.Value != values[EnvStateDir] {
		t.Fatalf("state override mismatch: %+v", resolved.StateDir)
	}
	if resolved.LogDir.Source != SourceEnv || resolved.LogDir.Value != values[EnvLogDir] {
		t.Fatalf("log override mismatch: %+v", resolved.LogDir)
	}
	if resolved.BlobDir.Source != SourceEnv || resolved.BlobDir.Value != values[EnvBlobDir] {
		t.Fatalf("blob override mismatch: %+v", resolved.BlobDir)
	}
}

func TestResolveDaemonStateDir_DefaultAndOverrides(t *testing.T) {
	home := t.TempDir()
	defaultState := ResolveDaemonStateDir(home, func(string) string { return "" })
	if defaultState != filepath.Join(home, ".local", "share", "ccsubagents", "state") {
		t.Fatalf("daemon state default mismatch: %q", defaultState)
	}

	values := map[string]string{
		localArtifactStateDirEnv: filepath.Join(t.TempDir(), "la-state"),
		EnvStateDir:              filepath.Join(t.TempDir(), "cc-state"),
	}
	got := ResolveDaemonStateDir(home, func(key string) string { return values[key] })
	if got != values[localArtifactStateDirEnv] {
		t.Fatalf("expected LOCAL_ARTIFACT_STATE_DIR precedence, got %q", got)
	}

	delete(values, localArtifactStateDirEnv)
	got = ResolveDaemonStateDir(home, func(key string) string { return values[key] })
	if got != values[EnvStateDir] {
		t.Fatalf("expected CCSUBAGENTS_STATE_DIR fallback, got %q", got)
	}
}
