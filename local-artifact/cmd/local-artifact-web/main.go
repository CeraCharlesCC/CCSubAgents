package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func main() {
	root, err := config.ResolveStoreRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine artifact store root:", err)
		os.Exit(1)
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine daemon state dir:", err)
		os.Exit(1)
	}
	logDir, err := config.ResolveLogDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine daemon log dir:", err)
		os.Exit(1)
	}

	addr := config.ResolveWebAddr()
	token := config.ResolveDaemonToken(stateDir)

	apiSocket := config.ResolveDaemonSocket(stateDir)
	apiAddr := config.ResolveDaemonAddr()
	if runtime.GOOS == "windows" {
		apiSocket = ""
	}

	if err := daemon.Run(context.Background(), daemon.RunConfig{
		StoreRoot: root,
		StateDir:  stateDir,
		LogDir:    logDir,
		APISocket: apiSocket,
		APIAddr:   apiAddr,
		WebAddr:   addr,
		Token:     token,
		Stderr:    os.Stderr,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "web daemon error:", err)
		os.Exit(1)
	}
}
