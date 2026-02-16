package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/bootstrap"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ccsubagents <install|update|uninstall>")
		os.Exit(2)
	}

	manager := bootstrap.NewManager()
	manager.SetStatusWriter(os.Stdout)
	command, err := bootstrap.ParseCommand(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := manager.Run(context.Background(), command); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
