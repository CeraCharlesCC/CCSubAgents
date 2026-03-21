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

type lifecycleArgs struct {
	scopeRaw              string
	versionRaw            string
	pinned                bool
	skipAttestationsCheck bool
	verbose               bool
	showUsage             bool
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if err := printUsage(stderr); err != nil {
			return 1
		}
		return 2
	}

	command := strings.TrimSpace(args[0])
	if command == "--help" || command == "-h" || command == "help" {
		if err := printUsage(stderr); err != nil {
			return 1
		}
		return 2
	}

	switch command {
	case "install", "update", "uninstall":
		return runLifecycle(command, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "daemon":
		return runDaemon(args[1:], stdout, stderr)
	case "artifacts":
		return runArtifacts(args[1:], os.Stdin, stdout, stderr)
	default:
		if err := writef(stderr, "unknown command %q\n", command); err != nil {
			return 1
		}
		if err := printUsage(stderr); err != nil {
			return 1
		}
		return 1
	}
}

func runLifecycle(command string, args []string, stdout, stderr io.Writer) int {
	parsed, err := parseLifecycleArgs(command, args)
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		if writeErr := printUsage(stderr); writeErr != nil {
			return 1
		}
		return 2
	}
	if parsed.showUsage {
		if err := printUsage(stderr); err != nil {
			return 1
		}
		return 2
	}

	parsedCommand, err := bootstrap.ParseCommand(command)
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		if writeErr := printUsage(stderr); writeErr != nil {
			return 1
		}
		return 1
	}
	scope, err := bootstrap.ResolveScope(parsedCommand, parsed.scopeRaw)
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		if writeErr := printUsage(stderr); writeErr != nil {
			return 1
		}
		return 1
	}

	err = bootstrap.Execute(context.Background(), bootstrap.ExecuteRequest{
		Command: parsedCommand,
		Scope:   scope,
		Options: bootstrap.ExecuteOptions{
			InstallVersion:        parsed.versionRaw,
			Pinned:                parsed.pinned,
			SkipAttestationsCheck: parsed.skipAttestationsCheck,
			Verbose:               parsed.verbose,
			StatusWriter:          stdout,
		},
	})
	if err != nil {
		if writeErr := writeln(stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func parseLifecycleArgs(command string, args []string) (lifecycleArgs, error) {
	fs := flag.NewFlagSet("ccsubagents "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	skipAttestationsCheck := fs.Bool("skip-attestations-check", false, "skip attestation verification")
	verbose := fs.Bool("verbose", false, "show detailed status output")
	scope := fs.String("scope", "", "scope for install lifecycle (local or global)")
	version := fs.String("version", "", "release version to install (for example v1.2.3)")
	pinned := fs.Bool("pinned", false, "pin the specified --version in settings.json")

	if err := fs.Parse(args); err != nil {
		return lifecycleArgs{}, err
	}
	if *help {
		return lifecycleArgs{showUsage: true}, nil
	}
	if fs.NArg() > 0 {
		return lifecycleArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if command != "install" && (strings.TrimSpace(*version) != "" || *pinned) {
		return lifecycleArgs{}, fmt.Errorf("--version and --pinned can only be used with install")
	}
	if *pinned && bootstrap.NormalizeInstallVersionTag(*version) == "" {
		return lifecycleArgs{}, bootstrap.ErrPinnedRequiresVersion
	}

	return lifecycleArgs{
		scopeRaw:              *scope,
		versionRaw:            *version,
		pinned:                *pinned,
		skipAttestationsCheck: *skipAttestationsCheck,
		verbose:               *verbose,
	}, nil
}

func printUsage(w io.Writer) error {
	const usage = `Usage: ccsubagents <command> [options]

Commands:
  install      Install agent definitions and local-artifact binaries
  update       Update an existing installation to the latest release
  uninstall    Remove installed files and revert configuration changes
  doctor       Run diagnostics for paths, daemon, binaries, and transaction state
  daemon       Manage daemon lifecycle (status, start, stop)
  artifacts    Manage daemon artifacts (ls, get, put, openwebui)

Lifecycle options (install/update/uninstall):
  --scope=local|global         Installation scope (default: install->local, update/uninstall->global)
  --version=<tag>              Install a specific release tag (install only)
  --pinned                     Save --version as pinned-version in settings.json (install only)
  --skip-attestations-check    Skip release attestation verification
  --verbose                    Show detailed output
  --help, -h                   Show this usage text

Examples:
  ccsubagents install
  ccsubagents update --scope=global
  ccsubagents doctor
  ccsubagents daemon status
  ccsubagents daemon start
  ccsubagents daemon stop
  ccsubagents artifacts ls --workspace-id=global
  ccsubagents artifacts get plan/demo --out=./demo.txt
  ccsubagents artifacts put plan/demo ./demo.txt --mime-type=text/plain
  ccsubagents artifacts openwebui
`

	_, err := io.WriteString(w, usage)
	return err
}
