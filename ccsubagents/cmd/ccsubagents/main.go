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
	versionRaw            string
	pinned                bool
	skipAttestationsCheck bool
	verbose               bool
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

	if err := bootstrap.Execute(context.Background(), bootstrap.ExecuteRequest{
		Command: command,
		Scope:   scope,
		Options: bootstrap.ExecuteOptions{
			InstallVersion:        parsed.versionRaw,
			Pinned:                parsed.pinned,
			SkipAttestationsCheck: parsed.skipAttestationsCheck,
			Verbose:               parsed.verbose,
			StatusWriter:          stdout,
		},
	}); err != nil {
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
	verbose := fs.Bool("verbose", false, "show detailed status output")
	scope := fs.String("scope", "", "scope for install lifecycle (local or global)")
	version := fs.String("version", "", "release version to install (for example v1.2.3)")
	pinned := fs.Bool("pinned", false, "pin the specified --version in settings.json")

	if err := fs.Parse(normalizeGlobalOptionOrder(args)); err != nil {
		return cliArgs{}, err
	}

	if *help {
		return cliArgs{showUsage: true, skipAttestationsCheck: *skipAttestationsCheck, verbose: *verbose}, nil
	}

	positionals := fs.Args()
	if len(positionals) != 1 {
		return cliArgs{}, fmt.Errorf("expected exactly 1 command argument (install, update, or uninstall)")
	}

	commandRaw := positionals[0]
	versionRaw := strings.TrimSpace(*version)
	if *pinned && bootstrap.NormalizeInstallVersionTag(versionRaw) == "" {
		return cliArgs{}, fmt.Errorf("--pinned requires --version")
	}
	if commandRaw != "install" && (versionRaw != "" || *pinned) {
		return cliArgs{}, fmt.Errorf("--version and --pinned can only be used with install")
	}

	return cliArgs{
		commandRaw:            commandRaw,
		scopeRaw:              *scope,
		versionRaw:            versionRaw,
		pinned:                *pinned,
		skipAttestationsCheck: *skipAttestationsCheck,
		verbose:               *verbose,
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
			// Handle options with values specially so a missing value does not consume
			// subsequent options during flag parsing.
			if (name == "scope" || name == "version") && !hasInlineValue {
				if idx+1 < len(args) {
					// Shell quoting is resolved before os.Args is built, so quoted
					// values (for example --scope="my scope") arrive as one token.
					if _, _, nextIsOption := parseOptionName(args[idx+1]); !nextIsOption {
						globalOptions = append(globalOptions, arg)
						globalOptions = append(globalOptions, args[idx+1])
						skipNext = true
					} else {
						globalOptions = append(globalOptions, "--"+name+"=")
					}
				} else {
					globalOptions = append(globalOptions, "--"+name+"=")
				}
				continue
			}
			globalOptions = append(globalOptions, arg)
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
	case "help", "h", "skip-attestations-check", "scope", "version", "pinned":
		return true
	case "verbose":
		return true
	default:
		return false
	}
}

func printUsage(w io.Writer) {
	const usage = `Usage: ccsubagents <command> [options]

Commands:
  install      Install agent definitions and local-artifact binaries
  update       Update an existing installation to the latest release
  uninstall    Remove installed files and revert configuration changes

Options:
  --scope=local|global         Installation scope (default: install→local, update/uninstall→global)
	--version=<tag>              Install a specific release tag (install only)
	--pinned                     Save --version as pinned-version in settings.json (install only)
  --skip-attestations-check    Skip release attestation verification
  --verbose                    Show detailed output
  --help, -h                   Show this usage text

Examples:
  ccsubagents install
  ccsubagents install --scope=global
	ccsubagents install --version=v1.2.3
	ccsubagents install --version=v1.2.3 --pinned
  ccsubagents update
  ccsubagents uninstall
  ccsubagents install --scope=global --verbose
`

	_, _ = io.WriteString(w, usage)
}
