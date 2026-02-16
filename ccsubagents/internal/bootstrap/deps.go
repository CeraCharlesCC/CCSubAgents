package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Manager struct {
	httpClient    *http.Client
	now           func() time.Time
	homeDir       func() (string, error)
	lookPath      func(string) (string, error)
	runCommand    func(context.Context, string, ...string) ([]byte, error)
	installBinary func(string, string) error
	statusOut     io.Writer
}

func NewManager() *Manager {
	return &Manager{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		now:           time.Now,
		homeDir:       os.UserHomeDir,
		lookPath:      exec.LookPath,
		runCommand:    runCommand,
		installBinary: installBinary,
	}
}

func (m *Manager) SetStatusWriter(writer io.Writer) {
	m.statusOut = writer
}

func (m *Manager) statusf(format string, args ...any) {
	if m.statusOut == nil {
		return
	}
	_, _ = fmt.Fprintf(m.statusOut, format, args...)
}

type installPaths struct {
	binaryDir    string
	settingsPath string
	mcpPath      string
}

func resolveInstallPaths(home string) installPaths {
	paths := installPaths{
		binaryDir:    filepath.Join(home, binaryInstallDirDefaultRel),
		settingsPath: filepath.Join(home, settingsRelativePath),
		mcpPath:      filepath.Join(home, mcpConfigRelativePath),
	}

	if override := resolveConfiguredPath(home, os.Getenv(binaryInstallDirEnv)); override != "" {
		paths.binaryDir = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(settingsPathEnv)); override != "" {
		paths.settingsPath = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(mcpConfigPathEnv)); override != "" {
		paths.mcpPath = override
	}

	return paths
}

func resolveConfiguredPath(home, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" {
		return filepath.Clean(home)
	}
	if strings.HasPrefix(trimmed, "~"+string(os.PathSeparator)) {
		return filepath.Join(home, trimmed[2:])
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(home, trimmed)
}

func toHomeTildePath(home, path string) string {
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(home)
	if cleanHome == "" || cleanHome == "." {
		return filepath.ToSlash(cleanPath)
	}

	rel, err := filepath.Rel(cleanHome, cleanPath)
	if err == nil {
		rel = filepath.Clean(rel)
		if rel == "." {
			return "~"
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}

	return filepath.ToSlash(cleanPath)
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return out, nil
}
