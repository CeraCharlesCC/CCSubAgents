package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCLIArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      cliArgs
		wantErr   string
		wantUsage bool
	}{
		{
			name: "install command",
			args: []string{"install"},
			want: cliArgs{commandRaw: "install"},
		},
		{
			name: "skip attestations flag before command",
			args: []string{"--skip-attestations-check", "update"},
			want: cliArgs{commandRaw: "update", skipAttestationsCheck: true},
		},
		{
			name: "verbose flag before command",
			args: []string{"--verbose", "install"},
			want: cliArgs{commandRaw: "install", verbose: true},
		},
		{
			name: "skip attestations flag after command",
			args: []string{"update", "--skip-attestations-check"},
			want: cliArgs{commandRaw: "update", skipAttestationsCheck: true},
		},
		{
			name: "verbose flag after command",
			args: []string{"update", "--verbose"},
			want: cliArgs{commandRaw: "update", verbose: true},
		},
		{
			name: "version flag before command",
			args: []string{"--version", "v1.2.3", "install"},
			want: cliArgs{commandRaw: "install", versionRaw: "v1.2.3"},
		},
		{
			name: "version flag after command",
			args: []string{"install", "--version", "v1.2.3"},
			want: cliArgs{commandRaw: "install", versionRaw: "v1.2.3"},
		},
		{
			name: "pinned with version",
			args: []string{"install", "--pinned", "--version", "v1.2.3"},
			want: cliArgs{commandRaw: "install", versionRaw: "v1.2.3", pinned: true},
		},
		{
			name: "pinned with version reversed order",
			args: []string{"install", "--version", "v1.2.3", "--pinned"},
			want: cliArgs{commandRaw: "install", versionRaw: "v1.2.3", pinned: true},
		},
		{
			name: "scope inline before command",
			args: []string{"--scope=global", "install"},
			want: cliArgs{commandRaw: "install", scopeRaw: "global"},
		},
		{
			name: "scope value after command",
			args: []string{"install", "--scope", "local"},
			want: cliArgs{commandRaw: "install", scopeRaw: "local"},
		},
		{
			name: "scope value token with spaces",
			args: []string{"install", "--scope", "my scope"},
			want: cliArgs{commandRaw: "install", scopeRaw: "my scope"},
		},
		{
			name:    "scope missing value before another flag",
			args:    []string{"install", "--scope", "--skip-attestations-check"},
			wantErr: "--scope requires a value",
		},
		{
			name:    "scope missing trailing value",
			args:    []string{"install", "--scope"},
			wantErr: "--scope requires a value",
		},
		{
			name:    "version missing value before another flag",
			args:    []string{"install", "--version", "--skip-attestations-check"},
			wantErr: "--version requires a value",
		},
		{
			name:    "version missing trailing value",
			args:    []string{"install", "--version"},
			wantErr: "--version requires a value",
		},
		{
			name: "help and skip attestations in mixed order",
			args: []string{"install", "--skip-attestations-check", "--help"},
			want: cliArgs{showUsage: true, skipAttestationsCheck: true},
		},
		{
			name:      "help long flag",
			args:      []string{"--help"},
			want:      cliArgs{showUsage: true},
			wantUsage: true,
		},
		{
			name:      "help short flag",
			args:      []string{"-h"},
			want:      cliArgs{showUsage: true},
			wantUsage: true,
		},
		{
			name:    "missing command",
			args:    []string{},
			wantErr: "expected exactly 1 command argument",
		},
		{
			name:    "extra positional",
			args:    []string{"install", "extra"},
			wantErr: "expected exactly 1 command argument",
		},
		{
			name:    "unknown flag",
			args:    []string{"--nope", "install"},
			wantErr: "flag provided but not defined",
		},
		{
			name:    "update rejects version",
			args:    []string{"update", "--version", "v1.2.3"},
			wantErr: "can only be used with install",
		},
		{
			name:    "install pinned requires version",
			args:    []string{"install", "--pinned"},
			wantErr: "--pinned requires --version",
		},
		{
			name:    "install pinned with none version requires version",
			args:    []string{"install", "--pinned", "--version", "none"},
			wantErr: "--pinned requires --version",
		},
		{
			name:    "install pinned with null version requires version",
			args:    []string{"install", "--pinned", "--version", "null"},
			wantErr: "--pinned requires --version",
		},
		{
			name:    "install pinned with whitespace version requires version",
			args:    []string{"install", "--pinned", "--version", "   "},
			wantErr: "--pinned requires --version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCLIArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCLIArgs returned error: %v", err)
			}
			if got.commandRaw != tc.want.commandRaw {
				t.Fatalf("expected commandRaw=%q, got %q", tc.want.commandRaw, got.commandRaw)
			}
			if got.scopeRaw != tc.want.scopeRaw {
				t.Fatalf("expected scopeRaw=%q, got %q", tc.want.scopeRaw, got.scopeRaw)
			}
			if got.versionRaw != tc.want.versionRaw {
				t.Fatalf("expected versionRaw=%q, got %q", tc.want.versionRaw, got.versionRaw)
			}
			if got.pinned != tc.want.pinned {
				t.Fatalf("expected pinned=%v, got %v", tc.want.pinned, got.pinned)
			}
			if got.skipAttestationsCheck != tc.want.skipAttestationsCheck {
				t.Fatalf("expected skipAttestationsCheck=%v, got %v", tc.want.skipAttestationsCheck, got.skipAttestationsCheck)
			}
			if got.verbose != tc.want.verbose {
				t.Fatalf("expected verbose=%v, got %v", tc.want.verbose, got.verbose)
			}
			if got.showUsage != tc.want.showUsage {
				t.Fatalf("expected showUsage=%v, got %v", tc.want.showUsage, got.showUsage)
			}
		})
	}
}

func TestRun_UsageAndCommandErrors(t *testing.T) {
	t.Run("help prints usage and exits 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := run([]string{"--help"}, &stdout, &stderr)
		if exit != 2 {
			t.Fatalf("expected exit code 2, got %d", exit)
		}
		out := stderr.String()
		if !strings.Contains(out, "Usage: ccsubagents <command> [options]") {
			t.Fatalf("expected usage text on stderr, got %q", out)
		}
		if !strings.Contains(out, "Commands:") {
			t.Fatalf("expected commands section in usage, got %q", out)
		}
		if !strings.Contains(out, "Options:") {
			t.Fatalf("expected options section in usage, got %q", out)
		}
		if !strings.Contains(out, "Examples:") {
			t.Fatalf("expected examples section in usage, got %q", out)
		}

		for _, want := range []string{
			"install",
			"update",
			"uninstall",
			"--scope=local|global",
			"--version=<tag>",
			"--pinned",
			"--skip-attestations-check",
			"--verbose",
			"--help, -h",
			"ccsubagents install",
			"ccsubagents install --scope=global",
			"ccsubagents install --version=v1.2.3",
			"ccsubagents install --version=v1.2.3 --pinned",
			"ccsubagents update",
			"ccsubagents uninstall",
			"ccsubagents install --scope=global --verbose",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected usage output to contain %q, got %q", want, out)
			}
		}
	})

	t.Run("invalid positional count prints usage and exits 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := run([]string{}, &stdout, &stderr)
		if exit != 2 {
			t.Fatalf("expected exit code 2, got %d", exit)
		}
		out := stderr.String()
		if !strings.Contains(out, "expected exactly 1 command argument") {
			t.Fatalf("expected argument count error, got %q", out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("expected usage text for invalid invocation, got %q", out)
		}
	})

	t.Run("pinned with none version prints usage and exits 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := run([]string{"install", "--pinned", "--version", "none"}, &stdout, &stderr)
		if exit != 2 {
			t.Fatalf("expected exit code 2, got %d", exit)
		}
		out := stderr.String()
		if !strings.Contains(out, "--pinned requires --version") {
			t.Fatalf("expected pinned/version usage error, got %q", out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("expected usage text for invalid invocation, got %q", out)
		}
	})

	t.Run("unknown command exits 1 and prints usage", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := run([]string{"upgrade"}, &stdout, &stderr)
		if exit != 1 {
			t.Fatalf("expected exit code 1, got %d", exit)
		}
		out := stderr.String()
		if !strings.Contains(out, "unknown command") {
			t.Fatalf("expected unknown command error, got %q", out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("expected usage text for command error, got %q", out)
		}
	})

	t.Run("unknown scope exits 1 and prints usage", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := run([]string{"install", "--scope=team"}, &stdout, &stderr)
		if exit != 1 {
			t.Fatalf("expected exit code 1, got %d", exit)
		}
		out := stderr.String()
		if !strings.Contains(out, "unknown scope") {
			t.Fatalf("expected unknown scope error, got %q", out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("expected usage text for scope error, got %q", out)
		}
	})
}
