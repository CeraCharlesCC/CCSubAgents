package installer

import (
	"context"
	"fmt"
)

func (r *Runner) Run(ctx context.Context, command Command, scope Scope) error {
	switch scope {
	case ScopeGlobal:
		return r.runGlobal(ctx, command)
	case ScopeLocal:
		return r.runLocal(ctx, command)
	default:
		return fmt.Errorf("unknown scope %q (expected: local, global)", scope)
	}
}

func (r *Runner) runGlobal(ctx context.Context, command Command) error {
	switch command {
	case CommandInstall:
		return r.runGlobalInstall(ctx)
	case CommandUpdate:
		r.globalInstallTargets = nil
		return r.installOrUpdate(ctx, true)
	case CommandUninstall:
		r.globalInstallTargets = nil
		return r.uninstall(ctx)
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
	switch command {
	case CommandInstall:
		return r.installLocal(ctx)
	case CommandUpdate:
		return r.updateLocal(ctx)
	case CommandUninstall:
		return r.uninstallLocal(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}
