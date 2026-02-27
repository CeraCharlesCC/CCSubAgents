package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func setTestHomeEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS != "windows" {
		return
	}

	volume := filepath.VolumeName(home)
	if volume == "" {
		volume = os.Getenv("HOMEDRIVE")
	}
	t.Setenv("HOMEDRIVE", volume)

	homePath := strings.TrimPrefix(home, volume)
	if homePath == "" {
		homePath = string(os.PathSeparator)
	}
	t.Setenv("HOMEPATH", homePath)
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(current)
	})
}

func TestRunArtifactsOpenWebUI_NoAuthIgnoresToken(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	setTestHomeEnv(t, home)
	withWorkingDir(t, cwd)

	t.Setenv("LOCAL_ARTIFACT_STATE_DIR", stateDir)
	t.Setenv("LOCAL_ARTIFACT_WEB_UI_ADDR", "")
	t.Setenv("LOCAL_ARTIFACT_DAEMON_TOKEN", "")
	t.Setenv("CCSUBAGENTS_CONFIG_DIR", filepath.Join(home, "config"))

	writeTestFile(t, filepath.Join(cwd, "ccsubagents", "settings.json"), `{"no-auth": true}`)
	writeTestFile(t, filepath.Join(stateDir, "daemon", "daemon.token"), "secret-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runArtifacts([]string{"openwebui"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runArtifacts exit code = %d, stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != "http://127.0.0.1:19130/\n" {
		t.Fatalf("openwebui URL mismatch: got=%q want=%q", got, "http://127.0.0.1:19130/\n")
	}
}

func TestRunArtifactsOpenWebUI_WithTokenWhenAuthEnabled(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	setTestHomeEnv(t, home)
	withWorkingDir(t, cwd)

	t.Setenv("LOCAL_ARTIFACT_STATE_DIR", stateDir)
	t.Setenv("LOCAL_ARTIFACT_WEB_UI_ADDR", "")
	t.Setenv("LOCAL_ARTIFACT_DAEMON_TOKEN", "")
	t.Setenv("CCSUBAGENTS_CONFIG_DIR", filepath.Join(home, "config"))

	writeTestFile(t, filepath.Join(cwd, "ccsubagents", "settings.json"), `{"no-auth": false, "webui-port": 19130}`)
	writeTestFile(t, filepath.Join(stateDir, "daemon", "daemon.token"), "abc123")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runArtifacts([]string{"openwebui"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runArtifacts exit code = %d, stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != "http://127.0.0.1:19130/?token=abc123\n" {
		t.Fatalf("openwebui URL mismatch: got=%q want=%q", got, "http://127.0.0.1:19130/?token=abc123\n")
	}
}
