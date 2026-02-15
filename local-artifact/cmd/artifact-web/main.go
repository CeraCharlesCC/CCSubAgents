package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"local-artifact-mcp/internal/presentation/web"
)

func main() {
	root := os.Getenv("ARTIFACT_STORE_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
			os.Exit(1)
		}
		root = filepath.Join(home, ".local", "share", "ccsubagents", "artifacts")
	}

	addr := os.Getenv("ARTIFACT_WEB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:19130"
	}

	srv := web.New(root)

	fmt.Fprintln(os.Stderr, "artifact web UI listening on http://"+addr)
	if err := srv.Serve(context.Background(), addr); err != nil {
		fmt.Fprintln(os.Stderr, "web server error:", err)
		os.Exit(1)
	}
}
