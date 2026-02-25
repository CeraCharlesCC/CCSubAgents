package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/mcp"
)

func main() {
	root, err := config.ResolveStoreRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine artifact store root:", err)
		os.Exit(1)
	}

	autostartWebUI, err := config.ResolveAutostartWebUI()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot resolve ccsubagents settings:", err)
		os.Exit(1)
	}
	if autostartWebUI {
		if err := startLocalArtifactWeb(os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to autostart local-artifact-web:", err)
		}
	}

	srv := mcp.New(root)

	// MCP stdio transport requires newline-delimited JSON-RPC messages on stdout.
	// Write any diagnostics only to stderr.
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

func startLocalArtifactWeb(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	webPath := localArtifactWebPath(exePath, runtime.GOOS)
	cmd := exec.Command(webPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			fmt.Fprintln(stderr, "local-artifact-web exited:", waitErr)
		}
	}()

	return nil
}

func localArtifactWebPath(exePath, goos string) string {
	name := "local-artifact-web"
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exePath), name)
}
