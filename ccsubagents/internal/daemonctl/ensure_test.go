package daemonctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

type fakeHealthClient struct {
	err error
}

func (f fakeHealthClient) Health(context.Context) error {
	return f.err
}

func TestWaitForStop_AcceptsExplicitAlreadyStoppedSignal(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "daemon already stopped"}}, nil
	}

	if err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond); err != nil {
		t.Fatalf("expected already-stopped success, got %v", err)
	}
}

func TestWaitForStop_AcceptsMissingSocketServiceUnavailable(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory"}}, nil
	}

	if err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond); err != nil {
		t.Fatalf("expected missing-socket stop success, got %v", err)
	}
}

func TestWaitForStop_RejectsNonStoppedServiceUnavailable(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: network is unreachable"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected non-stopped service unavailable failure")
	}
	if !strings.Contains(err.Error(), "stop verification failed") {
		t.Fatalf("expected verification failure context, got %v", err)
	}
}

func TestWaitForStop_RejectsUnauthorizedHealthError(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeUnauthorized, Message: "missing or invalid token"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected unauthorized health failure")
	}
	if !strings.Contains(err.Error(), "stop verification failed") {
		t.Fatalf("expected verification failure context, got %v", err)
	}
}

func TestWaitForStop_RejectsTransientHealthError(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "gateway timeout"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected transient health failure")
	}
	var remoteErr *daemonclient.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != daemonclient.CodeInternal {
		t.Fatalf("expected wrapped internal remote error, got %v", err)
	}
}

func TestResolveDaemonPath_PrefersSiblingBinary(t *testing.T) {
	base := t.TempDir()
	exePath := filepath.Join(base, "ccsubagents")
	sibling := filepath.Join(base, "ccsubagentsd")
	if err := os.WriteFile(sibling, []byte("daemon"), 0o755); err != nil {
		t.Fatalf("seed sibling daemon: %v", err)
	}

	got := resolveDaemonPath(exePath, t.TempDir(), "linux",
		func(string) string { return "" },
		func(string) (string, error) { return "/usr/local/bin/ccsubagentsd", nil },
	)
	if got != sibling {
		t.Fatalf("expected sibling daemon path %q, got %q", sibling, got)
	}
}

func TestResolveDaemonPath_UsesConfiguredBinDirWhenSiblingMissing(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create configured bin dir: %v", err)
	}
	configured := filepath.Join(binDir, "ccsubagentsd")
	if err := os.WriteFile(configured, []byte("daemon"), 0o755); err != nil {
		t.Fatalf("seed configured daemon: %v", err)
	}

	got := resolveDaemonPath(filepath.Join(t.TempDir(), "ccsubagents"), home, "linux",
		func(key string) string {
			if key == "LOCAL_ARTIFACT_BIN_DIR" {
				return "~/bin"
			}
			return ""
		},
		func(string) (string, error) { return "", errors.New("missing") },
	)
	if got != configured {
		t.Fatalf("expected configured daemon path %q, got %q", configured, got)
	}
}

func TestResolveDaemonPath_UsesLookPathWhenNoLocalCandidates(t *testing.T) {
	found := "/opt/tools/ccsubagentsd"
	got := resolveDaemonPath(filepath.Join(t.TempDir(), "ccsubagents"), t.TempDir(), "linux",
		func(string) string { return "" },
		func(string) (string, error) { return found, nil },
	)
	if got != found {
		t.Fatalf("expected lookPath daemon %q, got %q", found, got)
	}
}

func TestResolveDaemonPath_FallsBackToDefaultLocalBin(t *testing.T) {
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("create local bin dir: %v", err)
	}
	defaultPath := filepath.Join(localBin, "ccsubagentsd")
	if err := os.WriteFile(defaultPath, []byte("daemon"), 0o755); err != nil {
		t.Fatalf("seed default daemon: %v", err)
	}

	got := resolveDaemonPath(filepath.Join(t.TempDir(), "ccsubagents"), home, "linux",
		func(string) string { return "" },
		func(string) (string, error) { return "", errors.New("missing") },
	)
	if got != defaultPath {
		t.Fatalf("expected default local-bin daemon path %q, got %q", defaultPath, got)
	}
}

func TestEnsureToken_DisableAuthPreservesTokenFile(t *testing.T) {
	stateDir := t.TempDir()
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("stale-token"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}

	token, err := ensureToken(stateDir, true)
	if err != nil {
		t.Fatalf("ensureToken disable auth returned error: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token when auth is disabled, got %q", token)
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(b)) != "stale-token" {
		t.Fatalf("expected token file to stay unchanged while auth transition is pending, got %q", string(b))
	}
}

func TestClearToken_EmptiesTokenFile(t *testing.T) {
	stateDir := t.TempDir()
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("stale-token"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}

	if err := clearToken(stateDir); err != nil {
		t.Fatalf("clearToken returned error: %v", err)
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(b)) != "" {
		t.Fatalf("expected token file to be empty after clearToken, got %q", string(b))
	}
}

func TestEnsureToken_AuthEnabledGeneratesWhenMissing(t *testing.T) {
	stateDir := t.TempDir()
	token, err := ensureToken(stateDir, false)
	if err != nil {
		t.Fatalf("ensureToken returned error: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatalf("expected non-empty generated token")
	}

	b, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(b)) != token {
		t.Fatalf("persisted token mismatch: got=%q want=%q", strings.TrimSpace(string(b)), token)
	}
}

func TestEnsureToken_AuthEnabledRegeneratesWhenFileEmpty(t *testing.T) {
	stateDir := t.TempDir()
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("seed empty token file: %v", err)
	}

	token, err := ensureToken(stateDir, false)
	if err != nil {
		t.Fatalf("ensureToken returned error: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatalf("expected regenerated non-empty token")
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		t.Fatalf("expected token file to be overwritten with regenerated token")
	}
}
