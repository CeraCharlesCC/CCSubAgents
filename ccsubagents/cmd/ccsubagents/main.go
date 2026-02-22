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
	scopeRaw              string
	skipAttestationsCheck bool
	showUsage             bool
}

func run(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseCLIArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
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

	scope, err := bootstrap.ResolveScope(command, parsed.scopeRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr)
		return 1
	}

	if err := manager.Run(context.Background(), command, scope); err != nil {
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
	scope := fs.String("scope", "", "scope for install lifecycle (local or global)")

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
		scopeRaw:              *scope,
		skipAttestationsCheck: *skipAttestationsCheck,
	}, nil
}

func normalizeGlobalOptionOrder(args []string) []string {
	globalOptions := make([]string, 0, len(args))
	others := make([]string, 0, len(args))
	skipNext := false

	for idx := 0; idx < len(args); idx++ {
		if skipNext {
			skipNext = false
			continue
		}

		arg := args[idx]
		name, hasInlineValue, isOption := parseOptionName(arg)
		if isOption && isGlobalOptionName(name) {
			globalOptions = append(globalOptions, arg)
			if name == "scope" && !hasInlineValue && idx+1 < len(args) {
				globalOptions = append(globalOptions, args[idx+1])
				skipNext = true
			}
			continue
		}
		others = append(others, arg)
	}

	return append(globalOptions, others...)
}

func parseOptionName(arg string) (string, bool, bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", false, false
	}
	name := strings.TrimLeft(arg, "-")
	if name == "" {
		return "", false, false
	}
	parts := strings.SplitN(name, "=", 2)
	if len(parts) == 2 {
		return parts[0], true, true
	}
	return parts[0], false, true
}

func isGlobalOptionName(name string) bool {
	switch name {
	case "help", "h", "skip-attestations-check", "scope":
		return true
	default:
		return false
	}
}

func printUsage(w io.Writer) {
	const usage = `Usage:
  ccsubagents [global options] <install|update|uninstall>

Global options:
  --scope=local|global       Scope for install/update/uninstall lifecycle
  --skip-attestations-check   Skip release attestation verification
  --help, -h                  Show this usage text

Default scope by command:
  install      local
  update       global
  uninstall    global

Global install target prompt (--scope=global):
  1. .vscode-server-insiders
  2. .vscode-server
  3. custom path(s)
`

	_, _ = io.WriteString(w, usage)
}
