package installer

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

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	pathutil "github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

const (
	stateDirPerm   = files.DefaultStateDirPerm
	stateFilePerm  = files.DefaultStateFilePerm
	binaryFilePerm = files.DefaultBinaryFilePerm

	assetAgentsZip         = "agents.zip"
	assetArtifactMCP       = "local-artifact-mcp"
	assetArtifactWeb       = "local-artifact-web"
	assetCCSubagentsd      = "ccsubagentsd"
	localArtifactTagPrefix = "local-artifact/"

	binaryInstallDirDefaultRel    = ".local/bin"
	binaryInstallDirEnv           = "LOCAL_ARTIFACT_BIN_DIR"
	settingsStableRelativePath    = ".vscode-server/data/Machine/settings.json"
	settingsInsidersRelativePath  = ".vscode-server-insiders/data/Machine/settings.json"
	settingsPathEnv               = "LOCAL_ARTIFACT_SETTINGS_PATH"
	mcpConfigStableRelativePath   = ".vscode-server/data/User/mcp.json"
	mcpConfigInsidersRelativePath = ".vscode-server-insiders/data/User/mcp.json"
	mcpConfigPathEnv              = "LOCAL_ARTIFACT_MCP_PATH"
)

type Command string

type Scope string

const (
	CommandInstall   Command = "install"
	CommandUpdate    Command = "update"
	CommandUninstall Command = "uninstall"

	ScopeLocal  Scope = "local"
	ScopeGlobal Scope = "global"
)

type installDestination string

const (
	installDestinationStable   installDestination = "stable"
	installDestinationInsiders installDestination = "insiders"
	installDestinationBoth     installDestination = "both"
)

type Runner struct {
	httpClient            *http.Client
	now                   func() time.Time
	homeDir               func() (string, error)
	workingDir            func() (string, error)
	lookPath              func(string) (string, error)
	runCommand            func(context.Context, string, ...string) ([]byte, error)
	getenv                func(string) string
	installBinary         func(string, string) error
	stopDaemonFn          func(context.Context) error
	statusOut             io.Writer
	promptIn              io.Reader
	promptOut             io.Writer
	installVersionRaw     string
	pinRequested          bool
	installSettingsRoot   string
	pendingPinWrite       *pendingPinWrite
	skipAttestationsCheck bool
	statusErr             error
	verbose               bool
	globalInstallTargets  []installConfigTarget
	installDestination    installDestination
}

type installPaths struct {
	binaryDir       string
	desktopStable   configPaths
	desktopInsiders configPaths
	stable          configPaths
	insiders        configPaths
}

type configPaths struct {
	settingsPath string
	mcpPath      string
}

type installConfigTarget struct {
	settingsPath string
	mcpPath      string
}

func NewRunner() *Runner {
	return &Runner{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		now:        time.Now,
		homeDir:    os.UserHomeDir,
		workingDir: os.Getwd,
		lookPath:   exec.LookPath,
		runCommand: runCommand,
		getenv:     os.Getenv,
		installBinary: func(src, dst string) error {
			return files.InstallBinaryWithinBase(src, dst, filepath.Dir(dst), binaryFilePerm)
		},
		promptIn:  os.Stdin,
		promptOut: os.Stdout,
	}
}

func (r *Runner) SetStatusWriter(writer io.Writer) {
	r.statusOut = writer
}

func (r *Runner) SetSkipAttestationsCheck(skip bool) {
	r.skipAttestationsCheck = skip
}

func (r *Runner) SetVerbose(verbose bool) {
	r.verbose = verbose
}

func (r *Runner) SetInstallPromptIO(input io.Reader, output io.Writer) {
	r.promptIn = input
	r.promptOut = output
}

func (r *Runner) SetInstallVersion(version string) {
	r.installVersionRaw = version
}

func (r *Runner) SetPinned(pinned bool) {
	r.pinRequested = pinned
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

func resolveInstallPaths(home string) installPaths {
	var desktopStable configPaths
	var desktopInsiders configPaths
	switch runtime.GOOS {
	case "windows":
		appDataDir := filepath.Join(home, "AppData", "Roaming")
		desktopStable = configPaths{
			settingsPath: filepath.Join(appDataDir, "Code", "User", "settings.json"),
			mcpPath:      filepath.Join(appDataDir, "Code", "User", "mcp.json"),
		}
		desktopInsiders = configPaths{
			settingsPath: filepath.Join(appDataDir, "Code - Insiders", "User", "settings.json"),
			mcpPath:      filepath.Join(appDataDir, "Code - Insiders", "User", "mcp.json"),
		}
	case "darwin":
		desktopStable = configPaths{
			settingsPath: filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json"),
			mcpPath:      filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"),
		}
		desktopInsiders = configPaths{
			settingsPath: filepath.Join(home, "Library", "Application Support", "Code - Insiders", "User", "settings.json"),
			mcpPath:      filepath.Join(home, "Library", "Application Support", "Code - Insiders", "User", "mcp.json"),
		}
	default:
		desktopStable = configPaths{
			settingsPath: filepath.Join(home, ".config", "Code", "User", "settings.json"),
			mcpPath:      filepath.Join(home, ".config", "Code", "User", "mcp.json"),
		}
		desktopInsiders = configPaths{
			settingsPath: filepath.Join(home, ".config", "Code - Insiders", "User", "settings.json"),
			mcpPath:      filepath.Join(home, ".config", "Code - Insiders", "User", "mcp.json"),
		}
	}

	paths := installPaths{
		binaryDir:       filepath.Join(home, binaryInstallDirDefaultRel),
		desktopStable:   desktopStable,
		desktopInsiders: desktopInsiders,
		stable: configPaths{
			settingsPath: filepath.Join(home, settingsStableRelativePath),
			mcpPath:      filepath.Join(home, mcpConfigStableRelativePath),
		},
		insiders: configPaths{
			settingsPath: filepath.Join(home, settingsInsidersRelativePath),
			mcpPath:      filepath.Join(home, mcpConfigInsidersRelativePath),
		},
	}

	if override := pathutil.ResolveConfiguredPath(home, os.Getenv(binaryInstallDirEnv)); override != "" {
		paths.binaryDir = override
	}
	if override := pathutil.ResolveConfiguredPath(home, os.Getenv(settingsPathEnv)); override != "" {
		paths.desktopStable.settingsPath = override
		paths.desktopInsiders.settingsPath = override
		paths.stable.settingsPath = override
		paths.insiders.settingsPath = override
	}
	if override := pathutil.ResolveConfiguredPath(home, os.Getenv(mcpConfigPathEnv)); override != "" {
		paths.desktopStable.mcpPath = override
		paths.desktopInsiders.mcpPath = override
		paths.stable.mcpPath = override
		paths.insiders.mcpPath = override
	}

	return paths
}

func exeSuffix(goos string) string {
	if strings.EqualFold(goos, "windows") {
		return ".exe"
	}
	return ""
}

func localArtifactBinaryNames(goos string) (mcp, web string) {
	suffix := exeSuffix(goos)
	return assetArtifactMCP + suffix, assetArtifactWeb + suffix
}

func ccsubagentsdBinaryName(goos string) string {
	return assetCCSubagentsd + exeSuffix(goos)
}

func localArtifactBundleAssetName(goos, goarch string) string {
	return fmt.Sprintf("local-artifact_%s_%s.zip", goos, goarch)
}

func installAssetNamesForRuntime() []string {
	return []string{assetAgentsZip, localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH)}
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
