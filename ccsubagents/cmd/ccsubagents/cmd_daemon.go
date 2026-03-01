package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonctl"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func runDaemon(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: ccsubagents daemon <status|start|stop>")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stateDir := paths.ResolveDaemonStateDir(home, os.Getenv)

	sub := strings.TrimSpace(args[0])
	switch sub {
	case "status":
		client, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := client.Health(context.Background()); err != nil {
			if daemonctl.IsDaemonStoppedOrUnavailable(err) {
				fmt.Fprintf(stdout, "daemon status: stopped\nstate dir: %s\n", stateDir)
				return 0
			}
			fmt.Fprintf(stdout, "daemon status: unavailable (%v)\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon status: ok\nstate dir: %s\n", stateDir)
		return 0
	case "start":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		settings, err := config.LoadMergedInstallSettings(home, cwd)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		storeRoot := resolveStoreRoot(home)
		if err := daemonctl.StartAndWait(context.Background(), stateDir, storeRoot, settings.NoAuth, stderr); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "daemon started")
		return 0
	case "stop":
		client, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if _, err := client.Shutdown(context.Background()); err != nil && !daemonctl.IsDaemonStoppedOrUnavailable(err) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := daemonctl.WaitForStop(context.Background(), stateDir, 4*time.Second); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := daemonctl.StopRegisteredProcesses(context.Background(), stateDir, []string{"web", "mcp"}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "daemon stopped")
		return 0
	default:
		fmt.Fprintf(stderr, "unknown daemon subcommand %q\n", sub)
		return 2
	}
}

func resolveStoreRoot(home string) string {
	if v := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_STORE_DIR")); v != "" {
		return v
	}
	return filepath.Join(home, ".local", "share", "ccsubagents", "artifacts")
}
