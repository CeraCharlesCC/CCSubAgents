package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonctl"
)

func setMissingDaemonSocket(t *testing.T) {
	t.Helper()
	base, err := os.MkdirTemp("", "ccsa-daemon-socket-")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(base); removeErr != nil {
			t.Fatalf("remove temp dir: %v", removeErr)
		}
	})
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", filepath.Join(base, "d.sock"))
}

func TestIsDaemonStoppedOrUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "missing unix socket means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory"},
			want: true,
		},
		{
			name: "connection refused means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial tcp 127.0.0.1:19131: connect: connection refused"},
			want: true,
		},
		{
			name: "actively refused means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial tcp 127.0.0.1:19131: connectex: No connection could be made because the target machine actively refused it."},
			want: true,
		},
		{
			name: "already unavailable means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "daemon already unavailable"},
			want: true,
		},
		{
			name: "already stopped means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "daemon already stopped"},
			want: true,
		},
		{
			name: "unauthorized is not stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeUnauthorized, Message: "missing or invalid token"},
			want: false,
		},
		{
			name: "non-stopped service unavailable message is not stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: network is unreachable"},
			want: false,
		},
		{
			name: "plain errors are not stopped",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := daemonctl.IsDaemonStoppedOrUnavailable(tc.err); got != tc.want {
				t.Fatalf("IsDaemonStoppedOrUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRunDaemonStop_MissingSocket_IsIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("LOCAL_ARTIFACT_STATE_DIR", stateDir)
	setMissingDaemonSocket(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runDaemon([]string{"stop"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDaemon stop exit=%d stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != "daemon stopped\n" {
		t.Fatalf("stdout mismatch: got=%q want=%q", got, "daemon stopped\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunDaemonStop_MissingSocket_StillRunsRegisteredProcessCleanup(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("LOCAL_ARTIFACT_STATE_DIR", stateDir)
	setMissingDaemonSocket(t)

	pidDir := filepath.Join(stateDir, "daemon", "processes", "web")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}
	invalidPIDFile := filepath.Join(pidDir, "abc.pid")
	if err := os.WriteFile(invalidPIDFile, []byte("invalid\n"), 0o600); err != nil {
		t.Fatalf("seed invalid pid file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runDaemon([]string{"stop"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDaemon stop exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid pid filename") {
		t.Fatalf("expected cleanup error context in stderr, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "SERVICE_UNAVAILABLE") {
		t.Fatalf("expected cleanup failure to be surfaced instead of dial error, got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout on failure, got %q", stdout.String())
	}
}
