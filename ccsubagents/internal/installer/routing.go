package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func (r *Runner) Run(ctx context.Context, command Command, scope Scope) error {
	r.statusErr = nil

	var err error
	switch scope {
	case ScopeGlobal:
		err = r.runGlobal(ctx, command)
	case ScopeLocal:
		err = r.runLocal(ctx, command)
	default:
		return fmt.Errorf("unknown scope %q (expected: local, global)", scope)
	}

	if err != nil {
		return err
	}

	return r.statusErr
}

func (r *Runner) runGlobal(ctx context.Context, command Command) error {
	home, err := r.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	layout := paths.Global(home)
	if stateOverride := strings.TrimSpace(getenv(paths.EnvStateDir)); stateOverride != "" {
		layout.StateDir = filepath.Clean(stateOverride)
	}
	if blobOverride := strings.TrimSpace(getenv(paths.EnvBlobDir)); blobOverride != "" {
		layout.BlobDir = filepath.Clean(blobOverride)
	}

	flowCommand := "global-" + string(command)
	stepID := flowCommand + ".flow"

	switch command {
	case CommandInstall:
		return executeFlowWithEngine(ctx, layout.StateDir, layout.BlobDir, "global-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.runGlobalInstall(stepCtx)
		})
	case CommandUpdate:
		r.globalInstallTargets = nil
		return executeFlowWithEngine(ctx, layout.StateDir, layout.BlobDir, "global-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.installOrUpdate(stepCtx, true)
		})
	case CommandUninstall:
		r.globalInstallTargets = nil
		return executeFlowWithEngine(ctx, layout.StateDir, layout.BlobDir, "global-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.uninstall(stepCtx)
		})
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}

func (r *Runner) runGlobalInstall(ctx context.Context) error {
	home, err := r.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	paths := resolveInstallPaths(home)
	targets, err := r.promptGlobalInstallTargets(ctx, home, paths)
	if err != nil {
		return err
	}

	r.globalInstallTargets = targets
	return r.installOrUpdate(ctx, false)
}

func (r *Runner) runLocal(ctx context.Context, command Command) error {
	stateDir, err := r.localTrackedStateDir()
	if err != nil {
		return err
	}

	blobDir := filepath.Join(stateDir, "blob")
	getenv := r.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if blobOverride := strings.TrimSpace(getenv(paths.EnvBlobDir)); blobOverride != "" {
		blobDir = filepath.Clean(blobOverride)
	}

	flowCommand := "local-" + string(command)
	stepID := flowCommand + ".flow"

	switch command {
	case CommandInstall:
		return executeFlowWithEngine(ctx, stateDir, blobDir, "local-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.installLocal(stepCtx)
		})
	case CommandUpdate:
		return executeFlowWithEngine(ctx, stateDir, blobDir, "local-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.updateLocal(stepCtx)
		})
	case CommandUninstall:
		return executeFlowWithEngine(ctx, stateDir, blobDir, "local-flow", flowCommand, stepID, func(stepCtx context.Context) error {
			return r.uninstallLocal(stepCtx)
		})
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}
