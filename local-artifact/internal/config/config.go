package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	storeRootEnv = "LOCAL_ARTIFACT_STORE_DIR"
	webAddrEnv   = "LOCAL_ARTIFACT_WEB_UI_ADDR"

	defaultStoreRootRelative = ".local/share/ccsubagents/artifacts"
	defaultWebAddr           = "127.0.0.1:19130"
)

func ResolveStoreRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv(storeRootEnv)); root != "" {
		return root, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, filepath.FromSlash(defaultStoreRootRelative)), nil
}

func ResolveWebAddr() string {
	if addr := strings.TrimSpace(os.Getenv(webAddrEnv)); addr != "" {
		return addr
	}
	return defaultWebAddr
}
