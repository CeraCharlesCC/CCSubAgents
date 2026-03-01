package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/mcp"
)

type daemonReadinessProber interface {
	Health(ctx context.Context) error
	List(ctx context.Context, req daemon.ListRequest) (daemon.ListResponse, error)
}

type childProcess struct {
	cmd           *exec.Cmd
	exited        <-chan error
	stopRequested atomic.Bool
}

var (
	startLocalArtifactWebFn = startLocalArtifactWeb
	stopChildProcessFn      = stopChildProcess
	sendChildGracefulFn     = sendProcessGraceful
	sendChildForceFn        = sendProcessForce
)

func main() {
	os.Exit(mainExitCode())
}

func mainExitCode() int {
	err := run()
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer stop()

	root, err := config.ResolveStoreRoot()
	if err != nil {
		return fmt.Errorf("cannot determine artifact store root: %w", err)
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		return fmt.Errorf("cannot determine daemon state dir: %w", err)
	}
	unregisterMCPPID, err := daemon.RegisterProcessPID(stateDir, "mcp", os.Getpid())
	if err != nil {
		return fmt.Errorf("cannot register mcp pid: %w", err)
	}
	defer func() {
		if unregisterErr := unregisterMCPPID(); unregisterErr != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to unregister mcp pid:", unregisterErr)
		}
	}()

	ccSettings, err := config.ResolveCCSubagentsSettings()
	if err != nil {
		return fmt.Errorf("cannot resolve ccsubagents settings: %w", err)
	}

	return runWithAutostartedWebChild(ccSettings.AutostartWebUI, os.Stderr, func() error {
		client, err := ensureDaemonAvailable(ctx, root, stateDir, ccSettings.NoAuth, os.Stderr)
		if err != nil {
			return fmt.Errorf("cannot start ccsubagentsd: %w", err)
		}

		srv := mcp.NewWithClient(root, client)

		// MCP stdio transport requires newline-delimited JSON-RPC messages on stdout.
		// Write any diagnostics only to stderr.
		if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	})
}

func runWithAutostartedWebChild(autostartWeb bool, stderr io.Writer, runFn func() error) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if runFn == nil {
		return errors.New("run function is required")
	}

	var webChild *childProcess
	if autostartWeb {
		var err error
		webChild, err = startLocalArtifactWebFn(stderr)
		if err != nil {
			fmt.Fprintln(stderr, "warning: failed to autostart local-artifact-web:", err)
		}
	}

	if webChild != nil {
		defer func() {
			if stopErr := stopChildProcessFn(webChild, 2*time.Second); stopErr != nil {
				fmt.Fprintln(stderr, "warning: failed to stop local-artifact-web:", stopErr)
			}
		}()
	}

	return runFn()
}

func ensureDaemonAvailable(ctx context.Context, storeRoot, stateDir string, disableAuth bool, stderr io.Writer) (*daemon.Client, error) {
	if stderr == nil {
		stderr = os.Stderr
	}

	token := ""
	fallbackToken := ""
	if !disableAuth {
		resolvedToken, err := daemon.ResolveOrCreateToken(stateDir, config.ResolveDaemonToken(stateDir))
		if err != nil {
			return nil, err
		}
		token = resolvedToken
	} else {
		fallbackToken = readPersistedDaemonToken(stateDir)
	}
	client := buildDaemonClient(stateDir, token)
	if err := daemonReady(ctx, client); err == nil {
		return client, nil
	}
	var fallbackClient *daemon.Client
	if fallbackToken != "" {
		fallbackClient = buildDaemonClient(stateDir, fallbackToken)
		if err := daemonReady(ctx, fallbackClient); err == nil {
			return fallbackClient, nil
		}
	}

	if err := startCCSubagentsd(stderr, storeRoot, stateDir, token); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := daemonReady(ctx, client); err == nil {
			return client, nil
		}
		if fallbackClient != nil {
			if err := daemonReady(ctx, fallbackClient); err == nil {
				return fallbackClient, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for ccsubagentsd readiness")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
}

func daemonReady(ctx context.Context, client daemonReadinessProber) error {
	if err := client.Health(ctx); err != nil {
		return err
	}
	_, err := client.List(ctx, daemon.ListRequest{
		Workspace: daemon.WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID},
		Limit:     1,
	})
	return err
}

func buildDaemonClient(stateDir, token string) *daemon.Client {
	if runtime.GOOS == "windows" {
		return daemon.NewHTTPClient("http://"+config.ResolveDaemonAddr(), token)
	}
	return daemon.NewUnixSocketClient(config.ResolveDaemonSocket(stateDir), token)
}

func startCCSubagentsd(stderr io.Writer, storeRoot, stateDir, token string) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	home, _ := os.UserHomeDir()
	daemonPath := ccsubagentsdPath(exePath, home, runtime.GOOS, os.Getenv, exec.LookPath)
	cmd := exec.Command(daemonPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(),
		"LOCAL_ARTIFACT_STORE_DIR="+storeRoot,
		"LOCAL_ARTIFACT_STATE_DIR="+stateDir,
		"LOCAL_ARTIFACT_DAEMON_TOKEN="+token,
	)
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_ADDR="+config.ResolveDaemonAddr())
	} else {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_SOCKET="+config.ResolveDaemonSocket(stateDir))
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			fmt.Fprintln(stderr, "ccsubagentsd exited:", waitErr)
		}
	}()
	return nil
}

func startLocalArtifactWeb(stderr io.Writer) (*childProcess, error) {
	if stderr == nil {
		stderr = os.Stderr
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}

	webPath := localArtifactWebPath(exePath, runtime.GOOS)
	cmd := exec.Command(webPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	child := &childProcess{cmd: cmd, exited: exited}

	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil && !child.stopRequested.Load() {
			fmt.Fprintln(stderr, "local-artifact-web exited:", waitErr)
		}
		exited <- waitErr
		close(exited)
	}()

	return child, nil
}

func stopChildProcess(child *childProcess, timeout time.Duration) error {
	if child == nil || child.cmd == nil || child.cmd.Process == nil {
		return nil
	}
	if child.exited == nil {
		return nil
	}

	if exited, err := waitForChildExit(child.exited, 0); exited {
		reportChildExitError(err)
		return nil
	}
	child.stopRequested.Store(true)

	var gracefulErr error
	if err := sendChildGracefulFn(child.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if exited, waitErr := waitForChildExit(child.exited, 0); exited {
			reportChildExitError(waitErr)
			return nil
		}
		gracefulErr = err
	}
	if exited, err := waitForChildExit(child.exited, timeout); exited {
		reportChildExitError(err)
		return nil
	}

	if err := sendChildForceFn(child.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if exited, waitErr := waitForChildExit(child.exited, 0); exited {
			reportChildExitError(waitErr)
			return nil
		}
		if gracefulErr != nil {
			return fmt.Errorf("force stop after graceful error (%v): %w", gracefulErr, err)
		}
		return err
	}
	if exited, err := waitForChildExit(child.exited, timeout); exited {
		reportChildExitError(err)
		return nil
	}

	if gracefulErr != nil {
		return fmt.Errorf("timed out waiting for local-artifact-web to exit after graceful error: %w", gracefulErr)
	}
	return errors.New("timed out waiting for local-artifact-web to exit")
}

func waitForChildExit(exited <-chan error, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		select {
		case err := <-exited:
			return true, err
		default:
			return false, nil
		}
	}
	select {
	case err := <-exited:
		return true, err
	case <-time.After(timeout):
		return false, nil
	}
}

func reportChildExitError(err error) {
	// Child exit logging is emitted by the cmd.Wait goroutine using the caller-selected stderr writer.
	_ = err
}

func localArtifactWebPath(exePath, goos string) string {
	name := "local-artifact-web"
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exePath), name)
}

func ccsubagentsdPath(exePath, home, goos string, getenv func(string) string, lookPath func(string) (string, error)) string {
	name := "ccsubagentsd"
	if goos == "windows" {
		name += ".exe"
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	sibling := filepath.Join(filepath.Dir(exePath), name)
	if pathExists(sibling) {
		return sibling
	}

	configuredBinDir := config.ResolveConfiguredPath(home, strings.TrimSpace(getenv("LOCAL_ARTIFACT_BIN_DIR")))
	if configuredBinDir != "" {
		configuredPath := filepath.Join(configuredBinDir, name)
		if pathExists(configuredPath) {
			return configuredPath
		}
	}

	if found, err := lookPath(name); err == nil && strings.TrimSpace(found) != "" {
		return found
	}

	if home != "" && goos != "windows" {
		defaultPath := filepath.Join(home, ".local", "bin", name)
		if pathExists(defaultPath) {
			return defaultPath
		}
		if configuredBinDir == "" {
			return defaultPath
		}
	}

	if configuredBinDir != "" {
		return filepath.Join(configuredBinDir, name)
	}

	return sibling
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func readPersistedDaemonToken(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
