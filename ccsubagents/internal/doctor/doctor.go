package doctor

import (
	"context"
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
	if err := writef(out, "paths.config=%s (%s)\n", resolved.ConfigDir.Value, resolved.ConfigDir.Source); err != nil {
		return issues, err
	}
	if err := writef(out, "paths.state=%s (%s)\n", resolved.StateDir.Value, resolved.StateDir.Source); err != nil {
		return issues, err
	}
	if err := writef(out, "paths.log=%s (%s)\n", resolved.LogDir.Value, resolved.LogDir.Source); err != nil {
		return issues, err
	}
	if err := writef(out, "paths.blob=%s (%s)\n", resolved.BlobDir.Value, resolved.BlobDir.Source); err != nil {
		return issues, err
	}
	if err := writef(out, "daemon.state=%s\n", daemonStateDir); err != nil {
		return issues, err
	}

	for _, bin := range []string{"local-artifact-mcp", "local-artifact-web", "ccsubagentsd"} {
		path, findErr := lookPath(bin)
		if findErr != nil {
			issues++
			if err := writef(out, "missing binary: %s\n", bin); err != nil {
				return issues, err
			}
			continue
		}
		if err := writef(out, "binary.%s=%s\n", bin, path); err != nil {
			return issues, err
		}
	}

	tokenPath := filepath.Join(daemonStateDir, "daemon", "daemon.token")
	if info, statErr := os.Stat(tokenPath); statErr != nil {
		issues++
		if err := writef(out, "daemon.token=missing (%v)\n", statErr); err != nil {
			return issues, err
		}
	} else {
		if err := writef(out, "daemon.token=%s mode=%#o\n", tokenPath, info.Mode().Perm()); err != nil {
			return issues, err
		}
	}

	client, clientErr := daemonclient.NewDefaultClient(daemonStateDir, getenv)
	if clientErr != nil {
		issues++
		if err := writef(out, "daemon.client=unavailable (%v)\n", clientErr); err != nil {
			return issues, err
		}
	} else if err := client.Health(ctx); err != nil {
		issues++
		if writeErr := writef(out, "daemon.health=unavailable (%v)\n", err); writeErr != nil {
			return issues, writeErr
		}
	} else {
		if err := writeln(out, "daemon.health=ok"); err != nil {
			return issues, err
		}
	}

	entries, readErr := os.ReadDir(filepath.Join(resolved.StateDir.Value, "tx"))
	if readErr == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), "-active.json") {
				issues++
				if err := writef(out, "transaction.active=%s\n", filepath.Join(resolved.StateDir.Value, "tx", entry.Name())); err != nil {
					return issues, err
				}
			}
		}
	}

	return issues, nil
}
