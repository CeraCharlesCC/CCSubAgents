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

var (
	newDefaultDaemonClientFn  = daemonclient.NewDefaultClient
	waitForDaemonStopFn       = daemonctl.WaitForStop
	stopRegisteredProcessesFn = daemonctl.StopRegisteredProcesses
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
	client, err := newDefaultDaemonClientFn(daemonStateDir, getenv)
	if err != nil {
		return fmt.Errorf("create daemon client: %w", err)
	}

	if _, err := client.Shutdown(ctx); err != nil && !isDaemonDownOrUnavailable(err) {
		return fmt.Errorf("shutdown daemon: %w", err)
	}
	if err := waitForDaemonStopFn(ctx, daemonStateDir, 4*time.Second); err != nil && !isDaemonDownOrUnavailable(err) {
		return fmt.Errorf("wait for daemon stop: %w", err)
	}
	if err := stopRegisteredProcessesFn(ctx, daemonStateDir, []string{"web", "mcp"}); err != nil {
		if daemonctl.IsOnlyProcessRegistryMetadataIssues(err) {
			r.reportWarning("Ignoring stale daemon process metadata", err.Error())
		} else {
			return fmt.Errorf("stop registered daemon processes: %w", err)
		}
	}

	return nil
}

func isDaemonDownOrUnavailable(err error) bool {
	return daemonctl.IsDaemonStoppedOrUnavailable(err)
}
