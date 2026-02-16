package bootstrap

import (
	"fmt"
	"strings"
)

type Command string

const (
	CommandInstall   Command = installCommand
	CommandUpdate    Command = updateCommand
	CommandUninstall Command = uninstallCommand
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
