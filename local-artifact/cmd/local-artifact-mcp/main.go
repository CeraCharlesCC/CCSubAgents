package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"local-artifact-mcp/internal/presentation/mcp"
)

func main() {
	root := os.Getenv("LOCAL_ARTIFACT_STORE_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
			os.Exit(1)
		}
		root = filepath.Join(home, ".local", "share", "ccsubagents", "artifacts")
	}

	srv := mcp.New(root)

	// MCP stdio transport requires newline-delimited JSON-RPC messages on stdout.
	// Write any diagnostics only to stderr.
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
