package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

type fakeHealthClient struct {
	err      error
	onHealth func()
}

type fakeSocketDaemon struct {
	server   *http.Server
	listener net.Listener
	socket   string
}

func (d *fakeSocketDaemon) Close() {
	if d == nil {
		return
	}
	if d.server != nil {
		_ = d.server.Close()
	}
	if d.listener != nil {
		_ = d.listener.Close()
	}
	if strings.TrimSpace(d.socket) != "" {
		_ = os.Remove(d.socket)
	}
}

func resetStartAndWaitHooks(t *testing.T) {
	t.Helper()
	originalStartProcess := startProcessFn
	originalReadyTimeout := startReadyTimeout
	originalPollInterval := startPollInterval
	originalNow := startNow
	t.Cleanup(func() {
		startProcessFn = originalStartProcess
		startReadyTimeout = originalReadyTimeout
		startPollInterval = originalPollInterval
		startNow = originalNow
	})
}

func newShortSocketPath(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("", "ccsa-daemonctl-")
	if err != nil {
		t.Fatalf("create temp socket base: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(base)
	})
	return filepath.Join(base, "daemon.sock")
}

func writeDaemonEnvelopeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func writeDaemonEnvelopeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func startFakeDaemonUnixSocket(t *testing.T, socketPath, expectedToken string, requireAuth bool) *fakeSocketDaemon {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("create socket parent dir: %v", err)
	}
	_ = os.Remove(socketPath)

	server := &http.Server{}
	authorized := func(r *http.Request) bool {
		if !requireAuth {
			return true
		}
		return strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer "+expectedToken
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/daemon/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeDaemonEnvelopeErr(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		writeDaemonEnvelopeOK(w, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/daemon/v1/artifacts/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeDaemonEnvelopeErr(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		if !authorized(r) {
			writeDaemonEnvelopeErr(w, http.StatusUnauthorized, daemonclient.CodeUnauthorized, "missing or invalid token")
			return
		}
		writeDaemonEnvelopeOK(w, map[string]any{"items": []map[string]any{}})
	})
	mux.HandleFunc("/daemon/v1/control/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeDaemonEnvelopeErr(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		if !authorized(r) {
			writeDaemonEnvelopeErr(w, http.StatusUnauthorized, daemonclient.CodeUnauthorized, "missing or invalid token")
			return
		}
		writeDaemonEnvelopeOK(w, map[string]any{"status": "stopping"})
		go func() {
			_ = server.Close()
			_ = os.Remove(socketPath)
		}()
	})
	server.Handler = mux

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	daemon := &fakeSocketDaemon{server: server, listener: listener, socket: socketPath}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(fmt.Sprintf("fake daemon serve failed: %v", err))
		}
	}()
	t.Cleanup(func() { daemon.Close() })
	return daemon
}

func startFakeDaemonUnixSocketCancelOnList(t *testing.T, socketPath string, cancel context.CancelFunc) *fakeSocketDaemon {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("create socket parent dir: %v", err)
	}
	_ = os.Remove(socketPath)

	server := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeDaemonEnvelopeErr(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		writeDaemonEnvelopeOK(w, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/daemon/v1/artifacts/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeDaemonEnvelopeErr(w, http.StatusMethodNotAllowed, daemonclient.CodeMethodNotAllowed, "method not allowed")
			return
		}
		cancel()
		writeDaemonEnvelopeErr(w, http.StatusServiceUnavailable, daemonclient.CodeServiceUnavailable, "daemon not ready")
	})
	server.Handler = mux

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	daemon := &fakeSocketDaemon{server: server, listener: listener, socket: socketPath}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(fmt.Sprintf("fake daemon serve failed: %v", err))
		}
	}()
	t.Cleanup(func() { daemon.Close() })
	return daemon
}

func (f fakeHealthClient) Health(context.Context) error {
	if f.onHealth != nil {
		f.onHealth()
	}
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

func TestStartAndWait_CanceledBeforeNormalStart_DoesNotStartProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	resetStartAndWaitHooks(t)
	stateDir := t.TempDir()
	storeRoot := t.TempDir()
	socketPath := newShortSocketPath(t)
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = startFakeDaemonUnixSocketCancelOnList(t, socketPath, cancel)

	startCalled := false
	startProcessFn = func(stateDir, storeRoot, token string, stderr io.Writer) error {
		startCalled = true
		return nil
	}

	err := StartAndWait(ctx, stateDir, storeRoot, false, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if startCalled {
		t.Fatal("expected startProcessFn not to be called after cancellation before normal start")
	}
}

func TestStartAndWait_DisableAuth_CanceledBeforeRestartStart_DoesNotStartProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	resetStartAndWaitHooks(t)
	stateDir := t.TempDir()
	storeRoot := t.TempDir()
	socketPath := newShortSocketPath(t)
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", socketPath)

	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}

	_ = startFakeDaemonUnixSocket(t, socketPath, "secret", true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	originalHealthClient := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = originalHealthClient })
	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{
			err:      &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "daemon already stopped"},
			onHealth: cancel,
		}, nil
	}

	startCalled := false
	startProcessFn = func(stateDir, storeRoot, token string, stderr io.Writer) error {
		startCalled = true
		return nil
	}

	err := StartAndWait(ctx, stateDir, storeRoot, true, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if startCalled {
		t.Fatal("expected startProcessFn not to be called after cancellation before restart start")
	}
}

func TestStartAndWait_CancellationReturnsContextError(t *testing.T) {
	resetStartAndWaitHooks(t)
	stateDir := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", newShortSocketPath(t))

	startProcessFn = func(stateDir, storeRoot, token string, stderr io.Writer) error {
		return nil
	}
	startReadyTimeout = 30 * time.Second
	startPollInterval = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- StartAndWait(ctx, stateDir, storeRoot, false, io.Discard)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartAndWait did not return promptly after cancellation")
	}
}

func TestStartAndWait_DisableAuth_RestartsAuthEnabledDaemonToNoAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	resetStartAndWaitHooks(t)
	stateDir := t.TempDir()
	storeRoot := t.TempDir()
	socketPath := newShortSocketPath(t)
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", socketPath)

	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}

	_ = startFakeDaemonUnixSocket(t, socketPath, "secret", true)

	startCalls := 0
	startProcessFn = func(stateDir, storeRoot, token string, stderr io.Writer) error {
		startCalls++
		if token != "" {
			return fmt.Errorf("expected empty token for disable-auth restart, got %q", token)
		}
		_ = startFakeDaemonUnixSocket(t, socketPath, "", false)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := StartAndWait(ctx, stateDir, storeRoot, true, io.Discard); err != nil {
		t.Fatalf("StartAndWait disable-auth restart failed: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("expected one restart startProcess call, got %d", startCalls)
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(b)) != "" {
		t.Fatalf("expected token file cleared after no-auth readiness, got %q", string(b))
	}

	client := daemonClientWithToken(stateDir, "")
	if err := daemonReady(context.Background(), client); err != nil {
		t.Fatalf("expected no-auth daemon readiness after restart, got %v", err)
	}
}

func TestStartAndWait_DisableAuth_DoesNotSucceedWhenRestartFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket-based test")
	}

	resetStartAndWaitHooks(t)
	stateDir := t.TempDir()
	storeRoot := t.TempDir()
	socketPath := newShortSocketPath(t)
	t.Setenv("LOCAL_ARTIFACT_DAEMON_SOCKET", socketPath)

	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}

	_ = startFakeDaemonUnixSocket(t, socketPath, "secret", true)

	startErr := errors.New("restart failed")
	startProcessFn = func(stateDir, storeRoot, token string, stderr io.Writer) error {
		if token != "" {
			return fmt.Errorf("expected empty token for disable-auth restart, got %q", token)
		}
		return startErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err := StartAndWait(ctx, stateDir, storeRoot, true, io.Discard)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected restart failure %v, got %v", startErr, err)
	}

	b, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		t.Fatalf("read token file: %v", readErr)
	}
	if strings.TrimSpace(string(b)) != "secret" {
		t.Fatalf("expected token file unchanged on restart failure, got %q", string(b))
	}
}
