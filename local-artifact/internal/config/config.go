package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	storeRootEnv    = "LOCAL_ARTIFACT_STORE_DIR"
	stateDirEnv     = "LOCAL_ARTIFACT_STATE_DIR"
	logDirEnv       = "LOCAL_ARTIFACT_LOG_DIR"
	ccStateDirEnv   = "CCSUBAGENTS_STATE_DIR"
	ccLogDirEnv     = "CCSUBAGENTS_LOG_DIR"
	webAddrEnv      = "LOCAL_ARTIFACT_WEB_UI_ADDR"
	daemonSocketEnv = "LOCAL_ARTIFACT_DAEMON_SOCKET"
	daemonAddrEnv   = "LOCAL_ARTIFACT_DAEMON_ADDR"
	daemonTokenEnv  = "LOCAL_ARTIFACT_DAEMON_TOKEN"

	defaultWebAddr          = "127.0.0.1:19130"
	defaultDaemonAddr       = "127.0.0.1:19131"
	defaultDaemonSocketName = "ccsubagentsd.sock"
)

func ResolveStoreRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv(storeRootEnv)); root != "" {
		return root, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(defaultGlobalBase(home), "artifacts"), nil
}

func ResolveWebAddr() string {
	settings, err := ResolveCCSubagentsSettings()
	if err != nil {
		return ResolveWebAddrWithSettings(CCSubagentsSettings{})
	}
	return ResolveWebAddrWithSettings(settings)
}

func ResolveWebAddrWithSettings(settings CCSubagentsSettings) string {
	if addr := strings.TrimSpace(os.Getenv(webAddrEnv)); addr != "" {
		return addr
	}
	if settings.WebUIPort != 0 {
		return fmt.Sprintf("127.0.0.1:%d", settings.WebUIPort)
	}
	return defaultWebAddr
}

func ResolveStateDir() (string, error) {
	if state := strings.TrimSpace(os.Getenv(stateDirEnv)); state != "" {
		return state, nil
	}
	if state := strings.TrimSpace(os.Getenv(ccStateDirEnv)); state != "" {
		return state, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(defaultGlobalBase(home), "state"), nil
}

func ResolveLogDir() (string, error) {
	if logDir := strings.TrimSpace(os.Getenv(logDirEnv)); logDir != "" {
		return logDir, nil
	}
	if logDir := strings.TrimSpace(os.Getenv(ccLogDirEnv)); logDir != "" {
		return logDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(defaultGlobalBase(home), "log"), nil
}

func ResolveDaemonSocket(stateDir string) string {
	if socket := strings.TrimSpace(os.Getenv(daemonSocketEnv)); socket != "" {
		return socket
	}
	return filepath.Join(stateDir, "daemon", defaultDaemonSocketName)
}

func ResolveDaemonAddr() string {
	if addr := strings.TrimSpace(os.Getenv(daemonAddrEnv)); addr != "" {
		return addr
	}
	return defaultDaemonAddr
}

func ResolveDaemonToken(stateDir string) string {
	if token := strings.TrimSpace(os.Getenv(daemonTokenEnv)); token != "" {
		return token
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func defaultGlobalBase(home string) string {
	return defaultGlobalBaseForGOOS(runtime.GOOS, home)
}

func defaultGlobalBaseForGOOS(goos, home string) string {
	cleanHome := filepath.Clean(strings.TrimSpace(home))
	if cleanHome == "" || cleanHome == "." {
		cleanHome = "."
	}

	switch goos {
	case "windows":
		return filepath.Join(cleanHome, "AppData", "Local", "ccsubagents")
	case "darwin":
		return filepath.Join(cleanHome, "Library", "Application Support", "ccsubagents")
	default:
		return filepath.Join(cleanHome, ".local", "share", "ccsubagents")
	}
}
