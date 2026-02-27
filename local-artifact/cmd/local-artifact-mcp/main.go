package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func main() {
	root, err := config.ResolveStoreRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine artifact store root:", err)
		os.Exit(1)
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine daemon state dir:", err)
		os.Exit(1)
	}

	autostartWebUI, err := config.ResolveAutostartWebUI()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot resolve ccsubagents settings:", err)
		os.Exit(1)
	}
	if autostartWebUI {
		if err := startLocalArtifactWeb(os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to autostart local-artifact-web:", err)
		}
	}

	client, err := ensureDaemonAvailable(context.Background(), root, stateDir, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot start ccsubagentsd:", err)
		os.Exit(1)
	}

	srv := mcp.NewWithClient(root, client)

	// MCP stdio transport requires newline-delimited JSON-RPC messages on stdout.
	// Write any diagnostics only to stderr.
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

func ensureDaemonAvailable(ctx context.Context, storeRoot, stateDir string, stderr io.Writer) (*daemon.Client, error) {
	if stderr == nil {
		stderr = os.Stderr
	}

	token, err := daemon.ResolveOrCreateToken(stateDir, config.ResolveDaemonToken(stateDir))
	if err != nil {
		return nil, err
	}
	client := buildDaemonClient(stateDir, token)
	if err := daemonReady(ctx, client); err == nil {
		return client, nil
	}

	if err := startCCSubagentsd(stderr, storeRoot, stateDir, token); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := daemonReady(ctx, client); err == nil {
			return client, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for ccsubagentsd readiness")
		}
		time.Sleep(120 * time.Millisecond)
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

func startLocalArtifactWeb(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	webPath := localArtifactWebPath(exePath, runtime.GOOS)
	cmd := exec.Command(webPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			fmt.Fprintln(stderr, "local-artifact-web exited:", waitErr)
		}
	}()

	return nil
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

	configuredBinDir := resolveConfiguredPath(home, strings.TrimSpace(getenv("LOCAL_ARTIFACT_BIN_DIR")))
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

func resolveConfiguredPath(home, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" {
		return filepath.Clean(home)
	}
	if strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "~\\") {
		remainder := strings.TrimLeft(trimmed[2:], `/\`)
		if remainder == "" {
			return filepath.Clean(home)
		}
		return filepath.Join(home, remainder)
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if os.PathSeparator == '\\' && (strings.HasPrefix(trimmed, `\`) || strings.HasPrefix(trimmed, "/")) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(home, trimmed)
}
