package bootstrap

import (
	"context"
	"fmt"
)

func (m *Manager) runGlobal(ctx context.Context, command Command) error {
	switch command {
	case CommandInstall:
		return m.runGlobalInstall(ctx)
	case CommandUpdate:
		m.globalInstallTargets = nil
		return m.installOrUpdate(ctx, true)
	case CommandUninstall:
		m.globalInstallTargets = nil
		return m.uninstall(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}

func (m *Manager) runGlobalInstall(ctx context.Context) error {
	home, err := m.homeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	paths := resolveInstallPaths(home)
	targets, err := m.promptGlobalInstallTargets(ctx, home, paths)
	if err != nil {
		return err
	}

	m.globalInstallTargets = targets
	return m.installOrUpdate(ctx, false)
}

func (m *Manager) runLocal(ctx context.Context, command Command) error {
	switch command {
	case CommandInstall:
		return m.installLocal(ctx)
	case CommandUpdate:
		return m.updateLocal(ctx)
	case CommandUninstall:
		return m.uninstallLocal(ctx)
	default:
		return fmt.Errorf("unknown command %q (expected: install, update, uninstall)", command)
	}
}
