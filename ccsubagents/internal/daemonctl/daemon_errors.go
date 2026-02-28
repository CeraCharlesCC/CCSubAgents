package daemonctl

import (
	"errors"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

// IsDaemonStoppedOrUnavailable reports whether the daemon is already down.
func IsDaemonStoppedOrUnavailable(err error) bool {
	var remoteErr *daemonclient.RemoteError
	if !errors.As(err, &remoteErr) {
		return false
	}

	msg := strings.ToLower(strings.TrimSpace(remoteErr.Message))
	if strings.Contains(msg, "already stopped") || strings.Contains(msg, "already unavailable") {
		return true
	}
	if remoteErr.Code != daemonclient.CodeServiceUnavailable {
		return false
	}
	return strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused")
}
