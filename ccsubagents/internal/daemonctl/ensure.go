package daemonctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

type daemonHealthClient interface {
	Health(context.Context) error
}

var newDefaultHealthClient = func(stateDir string) (daemonHealthClient, error) {
	return daemonclient.NewDefaultClient(stateDir, os.Getenv)
}

var (
	startProcessFn    = startProcess
	startReadyTimeout = 8 * time.Second
	startPollInterval = 120 * time.Millisecond
	startNow          = time.Now
)

func StartAndWait(ctx context.Context, stateDir, storeRoot string, disableAuth bool, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fallbackToken := ""
	if disableAuth {
		fallbackToken = readPersistedDaemonToken(stateDir)
	}
	token, err := ensureToken(stateDir, disableAuth)
	if err != nil {
		return err
	}
	client := daemonClientWithToken(stateDir, token)
	var fallbackClient *daemonclient.Client
	if disableAuth && fallbackToken != "" {
		fallbackClient = daemonClientWithToken(stateDir, fallbackToken)
	}
	if err := daemonReady(ctx, client); err == nil {
		if disableAuth {
			return clearToken(stateDir)
		}
		return nil
	}
	if disableAuth && fallbackClient != nil {
		if err := daemonReady(ctx, fallbackClient); err == nil {
			if _, err := fallbackClient.Shutdown(ctx); err != nil && !IsDaemonStoppedOrUnavailable(err) {
				return err
			}
			if err := WaitForStop(ctx, stateDir, 4*time.Second); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := startProcessFn(stateDir, storeRoot, token, stderr); err != nil {
				return err
			}
			return waitForStartReady(ctx, client, stateDir, disableAuth)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := startProcessFn(stateDir, storeRoot, token, stderr); err != nil {
		return err
	}
	return waitForStartReady(ctx, client, stateDir, disableAuth)
}

func waitForStartReady(ctx context.Context, client *daemonclient.Client, stateDir string, disableAuth bool) error {
	deadline := startNow().Add(startReadyTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := daemonReady(ctx, client); err == nil {
			if disableAuth {
				return clearToken(stateDir)
			}
			return nil
		}
		if startNow().After(deadline) {
			return errors.New("timed out waiting for daemon readiness")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(startPollInterval):
		}
	}
}

func daemonReady(ctx context.Context, client *daemonclient.Client) error {
	if err := client.Health(ctx); err != nil {
		return err
	}
	_, err := client.List(ctx, daemonclient.ListRequest{
		Workspace: daemonclient.WorkspaceSelector{WorkspaceID: "global"},
		Limit:     1,
	})
	return err
}

func daemonClientWithToken(stateDir, token string) *daemonclient.Client {
	if runtime.GOOS == "windows" {
		addr := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_DAEMON_ADDR"))
		if addr == "" {
			addr = defaultDaemonAddr()
		}
		return daemonclient.NewHTTPClient("http://"+addr, token)
	}
	socket := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_DAEMON_SOCKET"))
	if socket == "" {
		socket = defaultDaemonSocket(stateDir)
	}
	return daemonclient.NewUnixSocketClient(socket, token)
}

func WaitForStop(ctx context.Context, stateDir string, timeout time.Duration) error {
	client, err := newDefaultHealthClient(stateDir)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := client.Health(ctx)
		if err != nil && IsDaemonStoppedOrUnavailable(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("daemon stop verification failed: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("daemon did not stop before timeout")
		}
		time.Sleep(120 * time.Millisecond)
	}
}
func startProcess(stateDir, storeRoot, token string, stderr io.Writer) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	daemonPath, err := resolveDaemonPath(exePath, home, runtime.GOOS, os.Getenv)
	if err != nil {
		return err
	}
	cmd := exec.Command(daemonPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(),
		"LOCAL_ARTIFACT_STORE_DIR="+storeRoot,
		"LOCAL_ARTIFACT_STATE_DIR="+stateDir,
		"LOCAL_ARTIFACT_DAEMON_TOKEN="+token,
	)
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_ADDR="+defaultDaemonAddr())
	} else {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_SOCKET="+defaultDaemonSocket(stateDir))
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon %s: %w", daemonPath, err)
	}
	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			_ = waitErr
		}
	}()
	return nil
}

func ensureToken(stateDir string, disableAuth bool) (string, error) {
	if disableAuth {
		return "", nil
	}
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")

	b, err := os.ReadFile(tokenPath)
	if err == nil {
		token := strings.TrimSpace(string(b))
		if token != "" {
			return token, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func clearToken(stateDir string) error {
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(tokenPath, []byte(""), 0o600)
}

func readPersistedDaemonToken(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func daemonBinaryNameForOS(goos string) string {
	if goos == "windows" {
		return "ccsubagentsd.exe"
	}
	return "ccsubagentsd"
}

func resolveDaemonPath(exePath, home, goos string, getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	name := daemonBinaryNameForOS(goos)
	sibling := filepath.Join(filepath.Dir(exePath), name)
	if pathExists(sibling) {
		return sibling, nil
	}

	checked := []string{sibling}
	configuredBinDir := paths.ResolveConfiguredPath(home, strings.TrimSpace(getenv("LOCAL_ARTIFACT_BIN_DIR")))
	if configuredBinDir != "" {
		configuredPath := filepath.Join(configuredBinDir, name)
		checked = appendPathIfMissing(checked, configuredPath)
		if pathExists(configuredPath) {
			return configuredPath, nil
		}
	}

	if home != "" {
		defaultPath := filepath.Join(home, ".local", "bin", name)
		checked = appendPathIfMissing(checked, defaultPath)
		if pathExists(defaultPath) {
			return defaultPath, nil
		}
	}

	return "", fmt.Errorf("cannot find daemon binary %q; checked %s", name, strings.Join(checked, ", "))
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func appendPathIfMissing(paths []string, candidate string) []string {
	if strings.TrimSpace(candidate) == "" {
		return paths
	}
	for _, existing := range paths {
		if filepath.Clean(existing) == filepath.Clean(candidate) {
			return paths
		}
	}
	return append(paths, candidate)
}

func defaultDaemonSocket(stateDir string) string {
	return filepath.Join(stateDir, "daemon", "ccsubagentsd.sock")
}

func defaultDaemonAddr() string {
	if addr := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_DAEMON_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:19131"
}
