package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func main() {
	storeRoot, err := config.ResolveStoreRoot()
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
	ccSettings, err := config.ResolveCCSubagentsSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot resolve ccsubagents settings:", err)
		os.Exit(1)
	}

	defaultSocket := config.ResolveDaemonSocket(stateDir)
	defaultAddr := config.ResolveDaemonAddr()
	defaultToken := config.ResolveDaemonToken(stateDir)

	var cfg daemon.RunConfig
	flag.StringVar(&cfg.StoreRoot, "store-root", storeRoot, "artifact store root")
	flag.StringVar(&cfg.StateDir, "state-dir", stateDir, "daemon state directory")
	flag.StringVar(&cfg.LogDir, "log-dir", logDir, "daemon log directory")
	flag.StringVar(&cfg.APISocket, "api-socket", defaultSocket, "daemon API unix socket path")
	flag.StringVar(&cfg.APIAddr, "api-addr", defaultAddr, "daemon API TCP address")
	flag.StringVar(&cfg.WebAddr, "web-addr", "", "optional web UI listen address (localhost only)")
	flag.StringVar(&cfg.Token, "token", defaultToken, "daemon auth token")
	flag.Parse()
	if ccSettings.NoAuth {
		cfg.Token = ""
		cfg.DisableAuth = true
	}

	if runtime.GOOS == "windows" {
		cfg.APISocket = ""
	}
	cfg.Stderr = os.Stderr

	if err := daemon.Run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ccsubagentsd error:", err)
		os.Exit(1)
	}
}
