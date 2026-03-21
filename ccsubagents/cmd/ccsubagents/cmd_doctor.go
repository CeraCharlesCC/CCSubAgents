package main

import (
	"context"
	"flag"
	"io"
	"os"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/doctor"
)

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ccsubagents doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 2
	}
	if *help {
		if err := writeln(stdout, "Usage: ccsubagents doctor"); err != nil {
			return 1
		}
		return 0
	}
	if fs.NArg() > 0 {
		if err := writef(stderr, "unexpected arguments: %v\n", fs.Args()); err != nil {
			return 1
		}
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}

	issues, err := doctor.Run(context.Background(), doctor.Options{
		Home: home,
		CWD:  cwd,
		Out:  stdout,
	})
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}
	if issues > 0 {
		return 1
	}
	return 0
}
