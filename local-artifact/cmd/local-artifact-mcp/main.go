package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/mcp"
)

func main() {
	root, err := config.ResolveStoreRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine artifact store root:", err)
		os.Exit(1)
	}

	srv := mcp.New(root)

	// MCP stdio transport requires newline-delimited JSON-RPC messages on stdout.
	// Write any diagnostics only to stderr.
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
