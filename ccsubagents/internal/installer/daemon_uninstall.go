package installer

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonctl"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func (r *Runner) stopDaemonBeforeRemoval(ctx context.Context) error {
	if r.stopDaemonFn != nil {
		return r.stopDaemonFn(ctx)
	}

	homeDir := r.homeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	daemonStateDir := paths.ResolveDaemonStateDir(home, getenv)
	client, err := daemonclient.NewDefaultClient(daemonStateDir, getenv)
	if err != nil {
		return fmt.Errorf("create daemon client: %w", err)
	}

	if _, err := client.Shutdown(ctx); err != nil && !isDaemonDownOrUnavailable(err) {
		return fmt.Errorf("shutdown daemon: %w", err)
	}
	if err := daemonctl.WaitForStop(ctx, daemonStateDir, 4*time.Second); err != nil && !isDaemonDownOrUnavailable(err) {
		return fmt.Errorf("wait for daemon stop: %w", err)
	}
	if err := daemonctl.StopRegisteredProcesses(ctx, daemonStateDir, []string{"web", "mcp"}); err != nil {
		return fmt.Errorf("stop registered daemon processes: %w", err)
	}

	return nil
}

func isDaemonDownOrUnavailable(err error) bool {
	return daemonctl.IsDaemonStoppedOrUnavailable(err)
}
