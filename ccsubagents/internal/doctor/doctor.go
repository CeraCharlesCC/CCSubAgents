package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

type Options struct {
	Home     string
	CWD      string
	Out      io.Writer
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

func Run(ctx context.Context, opts Options) (issues int, err error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	resolved := paths.Resolve(opts.Home, opts.CWD, getenv)
	daemonStateDir := paths.ResolveDaemonStateDir(opts.Home, getenv)
	fmt.Fprintf(out, "paths.config=%s (%s)\n", resolved.ConfigDir.Value, resolved.ConfigDir.Source)
	fmt.Fprintf(out, "paths.state=%s (%s)\n", resolved.StateDir.Value, resolved.StateDir.Source)
	fmt.Fprintf(out, "paths.log=%s (%s)\n", resolved.LogDir.Value, resolved.LogDir.Source)
	fmt.Fprintf(out, "paths.blob=%s (%s)\n", resolved.BlobDir.Value, resolved.BlobDir.Source)
	fmt.Fprintf(out, "daemon.state=%s\n", daemonStateDir)

	for _, bin := range []string{"local-artifact-mcp", "local-artifact-web", "ccsubagentsd"} {
		path, findErr := lookPath(bin)
		if findErr != nil {
			issues++
			fmt.Fprintf(out, "missing binary: %s\n", bin)
			continue
		}
		fmt.Fprintf(out, "binary.%s=%s\n", bin, path)
	}

	tokenPath := filepath.Join(daemonStateDir, "daemon", "daemon.token")
	if info, statErr := os.Stat(tokenPath); statErr != nil {
		issues++
		fmt.Fprintf(out, "daemon.token=missing (%v)\n", statErr)
	} else {
		fmt.Fprintf(out, "daemon.token=%s mode=%#o\n", tokenPath, info.Mode().Perm())
	}

	client, clientErr := daemonclient.NewDefaultClient(daemonStateDir, getenv)
	if clientErr != nil {
		issues++
		fmt.Fprintf(out, "daemon.client=unavailable (%v)\n", clientErr)
	} else if err := client.Health(ctx); err != nil {
		issues++
		fmt.Fprintf(out, "daemon.health=unavailable (%v)\n", err)
	} else {
		fmt.Fprintln(out, "daemon.health=ok")
	}

	entries, readErr := os.ReadDir(filepath.Join(resolved.StateDir.Value, "tx"))
	if readErr == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), "-active.json") {
				issues++
				fmt.Fprintf(out, "transaction.active=%s\n", filepath.Join(resolved.StateDir.Value, "tx", entry.Name()))
			}
		}
	}

	return issues, nil
}
