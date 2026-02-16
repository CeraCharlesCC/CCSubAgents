package main

import (
	"context"
	"fmt"
	"os"

	"ccsubagents/internal/bootstrap"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ccsubagents <install|update|uninstall>")
		os.Exit(2)
	}

	manager := bootstrap.NewManager()
	if err := manager.Run(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
