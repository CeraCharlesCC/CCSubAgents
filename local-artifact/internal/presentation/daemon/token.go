package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const tokenFileName = "daemon.token"

func tokenFilePath(stateDir string) string {
	return filepath.Join(stateDir, "daemon", tokenFileName)
}

func ResolveOrCreateToken(stateDir, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if err := os.MkdirAll(filepath.Dir(tokenFilePath(stateDir)), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(tokenFilePath(stateDir), []byte(explicit), 0o600); err != nil {
			return "", err
		}
		return explicit, nil
	}
	if token, err := ReadToken(stateDir); err == nil {
		return token, nil
	}
	if err := os.MkdirAll(filepath.Dir(tokenFilePath(stateDir)), 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenFilePath(stateDir), []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func ReadToken(stateDir string) (string, error) {
	b, err := os.ReadFile(tokenFilePath(stateDir))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("empty daemon token")
	}
	return token, nil
}
