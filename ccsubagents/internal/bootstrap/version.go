package bootstrap

import "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"

func NormalizeInstallVersionTag(raw string) string {
	return config.NormalizeInstallVersionTag(raw)
}
