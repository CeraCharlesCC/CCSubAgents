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
	"runtime"
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
	goos                  string
	goarch                string
	installBinary         func(string, string) error
	statusOut             io.Writer
	promptIn              io.Reader
	promptOut             io.Writer
	installVersionRaw     string
	pinRequested          bool
	installSettingsRoot   string
	pendingPinWrite       *pendingPinWrite
	skipAttestationsCheck bool
	verbose               bool
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
		goos:          runtime.GOOS,
		goarch:        runtime.GOARCH,
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

func (m *Manager) SetVerbose(verbose bool) {
	m.verbose = verbose
}

func (m *Manager) SetInstallPromptIO(input io.Reader, output io.Writer) {
	m.promptIn = input
	m.promptOut = output
}

func (m *Manager) SetInstallVersion(version string) {
	m.installVersionRaw = version
}

func (m *Manager) SetPinned(pinned bool) {
	m.pinRequested = pinned
}

func (m *Manager) statusf(format string, args ...any) {
	if m.statusOut == nil {
		return
	}
	_, _ = fmt.Fprintf(m.statusOut, format, args...)
}

type installPaths struct {
	binaryDir       string
	stable          configPaths
	insiders        configPaths
	desktopStable   configPaths
	desktopInsiders configPaths
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
	return resolveInstallPathsForOS(home, runtime.GOOS, os.Getenv("APPDATA"))
}

func resolveInstallPathsForOS(home, goos, appData string) installPaths {
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

	switch strings.TrimSpace(goos) {
	case "windows":
		roaming := strings.TrimSpace(appData)
		if roaming == "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		stableRoot := filepath.Join(roaming, "Code")
		insidersRoot := filepath.Join(roaming, "Code - Insiders")
		paths.desktopStable = configPaths{
			settingsPath: filepath.Join(stableRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(stableRoot, "User", "mcp.json"),
		}
		paths.desktopInsiders = configPaths{
			settingsPath: filepath.Join(insidersRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(insidersRoot, "User", "mcp.json"),
		}
	case "darwin":
		stableRoot := filepath.Join(home, "Library", "Application Support", "Code")
		insidersRoot := filepath.Join(home, "Library", "Application Support", "Code - Insiders")
		paths.desktopStable = configPaths{
			settingsPath: filepath.Join(stableRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(stableRoot, "User", "mcp.json"),
		}
		paths.desktopInsiders = configPaths{
			settingsPath: filepath.Join(insidersRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(insidersRoot, "User", "mcp.json"),
		}
	default:
		stableRoot := filepath.Join(home, ".config", "Code")
		insidersRoot := filepath.Join(home, ".config", "Code - Insiders")
		paths.desktopStable = configPaths{
			settingsPath: filepath.Join(stableRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(stableRoot, "User", "mcp.json"),
		}
		paths.desktopInsiders = configPaths{
			settingsPath: filepath.Join(insidersRoot, "User", "settings.json"),
			mcpPath:      filepath.Join(insidersRoot, "User", "mcp.json"),
		}
	}

	if override := resolveConfiguredPath(home, os.Getenv(binaryInstallDirEnv)); override != "" {
		paths.binaryDir = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(settingsPathEnv)); override != "" {
		paths.stable.settingsPath = override
		paths.insiders.settingsPath = override
		paths.desktopStable.settingsPath = override
		paths.desktopInsiders.settingsPath = override
	}
	if override := resolveConfiguredPath(home, os.Getenv(mcpConfigPathEnv)); override != "" {
		paths.stable.mcpPath = override
		paths.insiders.mcpPath = override
		paths.desktopStable.mcpPath = override
		paths.desktopInsiders.mcpPath = override
	}

	return paths
}

func (m *Manager) currentGOOS() string {
	if m == nil {
		return runtime.GOOS
	}
	if trimmed := strings.TrimSpace(m.goos); trimmed != "" {
		return trimmed
	}
	return runtime.GOOS
}

func (m *Manager) currentGOARCH() string {
	if m == nil {
		return runtime.GOARCH
	}
	if trimmed := strings.TrimSpace(m.goarch); trimmed != "" {
		return trimmed
	}
	return runtime.GOARCH
}

func (m *Manager) getenvOrDefault(key string) string {
	if m != nil && m.getenv != nil {
		return m.getenv(key)
	}
	return os.Getenv(key)
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
