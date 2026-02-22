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
	httpClient            *http.Client
	now                   func() time.Time
	homeDir               func() (string, error)
	workingDir            func() (string, error)
	lookPath              func(string) (string, error)
	runCommand            func(context.Context, string, ...string) ([]byte, error)
	getenv                func(string) string
	installBinary         func(string, string) error
	statusOut             io.Writer
	promptIn              io.Reader
	promptOut             io.Writer
	skipAttestationsCheck bool
	globalInstallTargets  []installConfigTarget
	installDestination    installDestination
}

func NewManager() *Manager {
	return &Manager{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		now:           time.Now,
		homeDir:       os.UserHomeDir,
		workingDir:    os.Getwd,
		lookPath:      exec.LookPath,
		runCommand:    runCommand,
		getenv:        os.Getenv,
		installBinary: installBinary,
		promptIn:      os.Stdin,
		promptOut:     os.Stdout,
	}
}

func (m *Manager) SetStatusWriter(writer io.Writer) {
	m.statusOut = writer
}

func (m *Manager) SetSkipAttestationsCheck(skip bool) {
	m.skipAttestationsCheck = skip
}

func (m *Manager) SetInstallPromptIO(input io.Reader, output io.Writer) {
	m.promptIn = input
	m.promptOut = output
}

func (m *Manager) statusf(format string, args ...any) {
	if m.statusOut == nil {
		return
	}
	_, _ = fmt.Fprintf(m.statusOut, format, args...)
}

type installPaths struct {
	binaryDir string
	stable    configPaths
	insiders  configPaths
}

type configPaths struct {
	settingsPath string
	mcpPath      string
}

type installConfigTarget struct {
	settingsPath string
	mcpPath      string
}

func resolveInstallPaths(home string) installPaths {
	paths := installPaths{
		binaryDir: filepath.Join(home, binaryInstallDirDefaultRel),
		stable: configPaths{
			settingsPath: filepath.Join(home, settingsStableRelativePath),
			mcpPath:      filepath.Join(home, mcpConfigStableRelativePath),
		},
		insiders: configPaths{
			settingsPath: filepath.Join(home, settingsInsidersRelativePath),
			mcpPath:      filepath.Join(home, mcpConfigInsidersRelativePath),
		},
	}

	if override := resolveConfiguredPath(home, os.Getenv(binaryInstallDirEnv)); override != "" {
		paths.binaryDir = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(settingsPathEnv)); override != "" {
		paths.stable.settingsPath = override
		paths.insiders.settingsPath = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(mcpConfigPathEnv)); override != "" {
		paths.stable.mcpPath = override
		paths.insiders.mcpPath = override
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
