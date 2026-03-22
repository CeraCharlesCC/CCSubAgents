package daemonapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const TokenFileName = "daemon.token"

func TokenFilePath(stateDir string) string {
	return filepath.Join(stateDir, "daemon", TokenFileName)
}

func ResolveOrCreateToken(stateDir, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if err := os.MkdirAll(filepath.Dir(TokenFilePath(stateDir)), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(TokenFilePath(stateDir), []byte(explicit), 0o600); err != nil {
			return "", err
		}
		return explicit, nil
	}
	if token, err := ReadToken(stateDir); err == nil {
		return token, nil
	}
	if err := os.MkdirAll(filepath.Dir(TokenFilePath(stateDir)), 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(TokenFilePath(stateDir), []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func ReadToken(stateDir string) (string, error) {
	b, err := os.ReadFile(TokenFilePath(stateDir))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("empty daemon token")
	}
	return token, nil
}
