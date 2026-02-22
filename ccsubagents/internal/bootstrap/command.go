package bootstrap

import (
	"fmt"
	"strings"
)

type Command string

type Scope string

const (
	CommandInstall   Command = installCommand
	CommandUpdate    Command = updateCommand
	CommandUninstall Command = uninstallCommand

	ScopeLocal  Scope = "local"
	ScopeGlobal Scope = "global"
)

func ParseCommand(raw string) (Command, error) {
	switch strings.TrimSpace(raw) {
	case installCommand:
		return CommandInstall, nil
	case updateCommand:
		return CommandUpdate, nil
	case uninstallCommand:
		return CommandUninstall, nil
	default:
		return "", fmt.Errorf("unknown command %q (expected: install, update, uninstall)", raw)
	}
}

func ParseScope(raw string) (Scope, error) {
	switch strings.TrimSpace(raw) {
	case string(ScopeLocal):
		return ScopeLocal, nil
	case string(ScopeGlobal):
		return ScopeGlobal, nil
	default:
		return "", fmt.Errorf("unknown scope %q (expected: local, global)", raw)
	}
}

func DefaultScope(command Command) Scope {
	switch command {
	case CommandInstall:
		return ScopeLocal
	case CommandUpdate, CommandUninstall:
		return ScopeGlobal
	default:
		return ScopeGlobal
	}
}

func ResolveScope(command Command, raw string) (Scope, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultScope(command), nil
	}
	return ParseScope(trimmed)
}
