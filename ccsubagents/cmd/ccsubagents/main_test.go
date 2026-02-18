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
			name: "skip attestations flag after command",
			args: []string{"update", "--skip-attestations-check"},
			want: cliArgs{commandRaw: "update", skipAttestationsCheck: true},
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
			if got.skipAttestationsCheck != tc.want.skipAttestationsCheck {
				t.Fatalf("expected skipAttestationsCheck=%v, got %v", tc.want.skipAttestationsCheck, got.skipAttestationsCheck)
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
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("expected usage text on stderr, got %q", out)
		}
		if !strings.Contains(out, "--skip-attestations-check") {
			t.Fatalf("expected skip option in usage, got %q", out)
		}
		if !strings.Contains(out, "1. .vscode-server") || !strings.Contains(out, "2. .vscode-server-insiders") || !strings.Contains(out, "3. both") {
			t.Fatalf("expected destination prompt details in usage, got %q", out)
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
}
