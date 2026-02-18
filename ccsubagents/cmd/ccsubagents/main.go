package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/bootstrap"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type cliArgs struct {
	commandRaw            string
	skipAttestationsCheck bool
	showUsage             bool
}

func run(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseCLIArgs(args)
	if err != nil {
		if err.Error() != "" {
			fmt.Fprintln(stderr, err)
		}
		printUsage(stderr)
		return 2
	}

	if parsed.showUsage {
		printUsage(stderr)
		return 2
	}

	manager := bootstrap.NewManager()
	manager.SetStatusWriter(stdout)
	manager.SetSkipAttestationsCheck(parsed.skipAttestationsCheck)

	command, err := bootstrap.ParseCommand(parsed.commandRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr)
		return 1
	}

	if err := manager.Run(context.Background(), command); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func parseCLIArgs(args []string) (cliArgs, error) {
	fs := flag.NewFlagSet("ccsubagents", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	skipAttestationsCheck := fs.Bool("skip-attestations-check", false, "skip attestation verification")

	if err := fs.Parse(normalizeGlobalOptionOrder(args)); err != nil {
		return cliArgs{}, err
	}

	if *help {
		return cliArgs{showUsage: true, skipAttestationsCheck: *skipAttestationsCheck}, nil
	}

	positionals := fs.Args()
	if len(positionals) != 1 {
		return cliArgs{}, fmt.Errorf("expected exactly 1 command argument (install, update, or uninstall)")
	}

	return cliArgs{
		commandRaw:            positionals[0],
		skipAttestationsCheck: *skipAttestationsCheck,
	}, nil
}

func normalizeGlobalOptionOrder(args []string) []string {
	globalOptions := make([]string, 0, len(args))
	others := make([]string, 0, len(args))

	for _, arg := range args {
		if isGlobalOption(arg) {
			globalOptions = append(globalOptions, arg)
			continue
		}
		others = append(others, arg)
	}

	return append(globalOptions, others...)
}

func isGlobalOption(arg string) bool {
	switch {
	case arg == "--help", arg == "-h", arg == "--skip-attestations-check":
		return true
	case strings.HasPrefix(arg, "--help="), strings.HasPrefix(arg, "-h="), strings.HasPrefix(arg, "--skip-attestations-check="):
		return true
	default:
		return false
	}
}

func printUsage(w io.Writer) {
	const usage = `Usage:
  ccsubagents [global options] <install|update|uninstall>

Global options:
  --skip-attestations-check   Skip release attestation verification
  --help, -h                  Show this usage text

Install destination prompt:
  install always prompts for destination:
    1. .vscode-server
    2. .vscode-insider-server
    3. both
`

	_, _ = io.WriteString(w, usage)
}
