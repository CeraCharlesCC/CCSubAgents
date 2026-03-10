package main

import (
	"context"
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
		if err := writeln(stderr, "Usage: ccsubagents daemon <status|start|stop>"); err != nil {
			return 1
		}
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}
	stateDir := paths.ResolveDaemonStateDir(home, os.Getenv)

	sub := strings.TrimSpace(args[0])
	switch sub {
	case "status":
		client, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
		if err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := client.Health(context.Background()); err != nil {
			if daemonctl.IsDaemonStoppedOrUnavailable(err) {
				if writeErr := writef(stdout, "daemon status: stopped\nstate dir: %s\n", stateDir); writeErr != nil {
					return 1
				}
				return 0
			}
			if writeErr := writef(stdout, "daemon status: unavailable (%v)\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := writef(stdout, "daemon status: ok\nstate dir: %s\n", stateDir); err != nil {
			return 1
		}
		return 0
	case "start":
		cwd, err := os.Getwd()
		if err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		settings, err := config.LoadMergedInstallSettings(home, cwd)
		if err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		storeRoot := resolveStoreRoot(home)
		if err := daemonctl.StartAndWait(context.Background(), stateDir, storeRoot, settings.NoAuth, stderr); err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := writeln(stdout, "daemon started"); err != nil {
			return 1
		}
		return 0
	case "stop":
		client, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
		if err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, err := client.Shutdown(context.Background()); err != nil && !daemonctl.IsDaemonStoppedOrUnavailable(err) {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := daemonctl.WaitForStop(context.Background(), stateDir, 4*time.Second); err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := daemonctl.StopRegisteredProcesses(context.Background(), stateDir, []string{"web", "mcp"}); err != nil {
			if writeErr := writeln(stderr, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := writeln(stdout, "daemon stopped"); err != nil {
			return 1
		}
		return 0
	default:
		if err := writef(stderr, "unknown daemon subcommand %q\n", sub); err != nil {
			return 1
		}
		return 2
	}
}

func resolveStoreRoot(home string) string {
	if v := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_STORE_DIR")); v != "" {
		return v
	}
	return filepath.Join(home, ".local", "share", "ccsubagents", "artifacts")
}
