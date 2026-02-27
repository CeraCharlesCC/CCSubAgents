package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	EnvConfigDir = "CCSUBAGENTS_CONFIG_DIR"
	EnvStateDir  = "CCSUBAGENTS_STATE_DIR"
	EnvLogDir    = "CCSUBAGENTS_LOG_DIR"
	EnvBlobDir   = "CCSUBAGENTS_BLOB_DIR"

	localArtifactStateDirEnv = "LOCAL_ARTIFACT_STATE_DIR"
)

type LayerSource string

const (
	SourceEnv       LayerSource = "env"
	SourceWorkspace LayerSource = "workspace"
	SourceGlobal    LayerSource = "global"
)

type PathSet struct {
	ConfigDir string
	StateDir  string
	LogDir    string
	BlobDir   string
}

type LayeredPath struct {
	Value  string
	Source LayerSource
}

type ResolvedPaths struct {
	WorkspaceRoot string
	Global        PathSet
	Workspace     PathSet
	ConfigDir     LayeredPath
	StateDir      LayeredPath
	LogDir        LayeredPath
	BlobDir       LayeredPath
}

func WorkspaceRoot(cwd string) string {
	cleanCwd := filepath.Clean(strings.TrimSpace(cwd))
	if cleanCwd == "" || cleanCwd == "." {
		cleanCwd = "."
	}
	if strings.EqualFold(filepath.Base(cleanCwd), "ccsubagents") {
		return cleanCwd
	}
	return filepath.Join(cleanCwd, "ccsubagents")
}

func Workspace(cwd string) PathSet {
	root := WorkspaceRoot(cwd)
	return PathSet{
		ConfigDir: root,
		StateDir:  root,
		LogDir:    filepath.Join(root, "log"),
		BlobDir:   filepath.Join(root, "blob"),
	}
}

func Global(home string) PathSet {
	cleanHome := filepath.Clean(strings.TrimSpace(home))
	if cleanHome == "" || cleanHome == "." {
		cleanHome = "."
	}

	base := defaultGlobalBase(cleanHome)
	return PathSet{
		ConfigDir: filepath.Join(base, "config"),
		StateDir:  base,
		LogDir:    filepath.Join(base, "log"),
		BlobDir:   filepath.Join(base, "blob"),
	}
}

func ResolveDaemonStateDir(home string, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := strings.TrimSpace(getenv(localArtifactStateDirEnv)); v != "" {
		return filepath.Clean(v)
	}
	if v := strings.TrimSpace(getenv(EnvStateDir)); v != "" {
		return filepath.Clean(v)
	}
	cleanHome := filepath.Clean(strings.TrimSpace(home))
	if cleanHome == "" || cleanHome == "." {
		cleanHome = "."
	}
	return filepath.Join(defaultGlobalBase(cleanHome), "state")
}

func Resolve(home, cwd string, getenv func(string) string) ResolvedPaths {
	if getenv == nil {
		getenv = os.Getenv
	}

	global := Global(home)
	workspaceRoot := WorkspaceRoot(cwd)
	workspace := Workspace(cwd)
	workspaceEnabled := dirExists(workspaceRoot)

	configDir, configSource := resolveField(getenv, EnvConfigDir, workspaceEnabled, workspace.ConfigDir, global.ConfigDir)
	stateDir, stateSource := resolveField(getenv, EnvStateDir, workspaceEnabled, workspace.StateDir, global.StateDir)
	logDir, logSource := resolveField(getenv, EnvLogDir, workspaceEnabled, workspace.LogDir, global.LogDir)
	blobDir, blobSource := resolveField(getenv, EnvBlobDir, workspaceEnabled, workspace.BlobDir, global.BlobDir)

	return ResolvedPaths{
		WorkspaceRoot: workspaceRoot,
		Global:        global,
		Workspace:     workspace,
		ConfigDir: LayeredPath{
			Value:  configDir,
			Source: configSource,
		},
		StateDir: LayeredPath{
			Value:  stateDir,
			Source: stateSource,
		},
		LogDir: LayeredPath{
			Value:  logDir,
			Source: logSource,
		},
		BlobDir: LayeredPath{
			Value:  blobDir,
			Source: blobSource,
		},
	}
}

func resolveField(getenv func(string) string, envVar string, workspaceEnabled bool, workspacePath string, globalPath string) (string, LayerSource) {
	if v := strings.TrimSpace(getenv(envVar)); v != "" {
		return filepath.Clean(v), SourceEnv
	}
	if workspaceEnabled {
		return filepath.Clean(workspacePath), SourceWorkspace
	}
	return filepath.Clean(globalPath), SourceGlobal
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func defaultGlobalBase(home string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Local", "ccsubagents")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ccsubagents")
	default:
		return filepath.Join(home, ".local", "share", "ccsubagents")
	}
}
