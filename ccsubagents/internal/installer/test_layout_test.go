package installer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

func globalStateDirForTest(home string) string {
	return paths.Global(home).StateDir
}

func globalAgentsDirForTest(home string) string {
	return filepath.Join(globalStateDirForTest(home), "agents")
}

func globalAgentsTildePathForTest(home string) string {
	home = filepath.Clean(home)
	agents := filepath.Clean(globalAgentsDirForTest(home))
	rel, err := filepath.Rel(home, agents)
	if err != nil {
		return filepath.ToSlash(agents)
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return "~"
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(agents)
	}
	return "~/" + filepath.ToSlash(rel)
}
