package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func main() {
	os.Exit(mainExitCode())
}

func mainExitCode() int {
	err := run()
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer stop()

	root, err := config.ResolveStoreRoot()
	if err != nil {
		return fmt.Errorf("cannot determine artifact store root: %w", err)
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		return fmt.Errorf("cannot determine daemon state dir: %w", err)
	}
	unregisterWebPID, err := daemon.RegisterProcessPID(stateDir, "web", os.Getpid())
	if err != nil {
		return fmt.Errorf("cannot register web pid: %w", err)
	}
	defer func() {
		if unregisterErr := unregisterWebPID(); unregisterErr != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to unregister web pid:", unregisterErr)
		}
	}()
	logDir, err := config.ResolveLogDir()
	if err != nil {
		return fmt.Errorf("cannot determine daemon log dir: %w", err)
	}
	ccSettings, err := config.ResolveCCSubagentsSettings()
	if err != nil {
		return fmt.Errorf("cannot resolve ccsubagents settings: %w", err)
	}

	addr := config.ResolveWebAddrWithSettings(ccSettings)
	token := config.ResolveDaemonToken(stateDir)
	if ccSettings.NoAuth {
		token = ""
	}

	apiSocket := config.ResolveDaemonSocket(stateDir)
	apiAddr := config.ResolveDaemonAddr()
	if runtime.GOOS == "windows" {
		apiSocket = ""
	}

	err = daemon.Run(ctx, daemon.RunConfig{
		StoreRoot:   root,
		StateDir:    stateDir,
		LogDir:      logDir,
		APISocket:   apiSocket,
		APIAddr:     apiAddr,
		WebAddr:     addr,
		Token:       token,
		DisableAuth: ccSettings.NoAuth,
		Stderr:      os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("web daemon error: %w", err)
	}
	return nil
}
