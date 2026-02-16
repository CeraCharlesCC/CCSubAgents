package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/web"
)

func main() {
	root, err := config.ResolveStoreRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine artifact store root:", err)
		os.Exit(1)
	}

	addr := config.ResolveWebAddr()

	srv := web.New(root)

	fmt.Fprintln(os.Stderr, "artifact web UI listening on http://"+addr)
	if err := srv.Serve(context.Background(), addr); err != nil {
		fmt.Fprintln(os.Stderr, "web server error:", err)
		os.Exit(1)
	}
}
