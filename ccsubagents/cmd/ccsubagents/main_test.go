package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseLifecycleArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		wantErr string
	}{
		{name: "install version pinned", command: "install", args: []string{"--version", "v1.2.3", "--pinned"}},
		{name: "update rejects version", command: "update", args: []string{"--version", "v1.2.3"}, wantErr: "can only be used with install"},
		{name: "pinned requires version", command: "install", args: []string{"--pinned"}, wantErr: "--pinned requires --version"},
		{name: "unexpected positional", command: "install", args: []string{"extra"}, wantErr: "unexpected arguments"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLifecycleArgs(tc.command, tc.args)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error mismatch: got=%q wantContains=%q", err.Error(), tc.wantErr)
				}
			}
		})
	}
}

func TestRun_UsageAndCommandErrors(t *testing.T) {
	t.Run("help prints usage", func(t *testing.T) {
		var out bytes.Buffer
		exit := run([]string{"--help"}, &out, &out)
		if exit != 2 {
			t.Fatalf("exit mismatch: got=%d want=2", exit)
		}
		text := out.String()
		for _, want := range []string{"doctor", "daemon", "artifacts", "install", "update", "uninstall"} {
			if !strings.Contains(text, want) {
				t.Fatalf("expected usage to contain %q, got %q", want, text)
			}
		}
	})

	t.Run("unknown command exits 1", func(t *testing.T) {
		var out bytes.Buffer
		exit := run([]string{"upgrade"}, &out, &out)
		if exit != 1 {
			t.Fatalf("exit mismatch: got=%d want=1", exit)
		}
		if !strings.Contains(out.String(), "unknown command") {
			t.Fatalf("expected unknown command error, got %q", out.String())
		}
	})

	t.Run("daemon missing subcommand exits 2", func(t *testing.T) {
		var out bytes.Buffer
		exit := run([]string{"daemon"}, &out, &out)
		if exit != 2 {
			t.Fatalf("exit mismatch: got=%d want=2", exit)
		}
	})

	t.Run("artifacts missing subcommand exits 2", func(t *testing.T) {
		var out bytes.Buffer
		exit := run([]string{"artifacts"}, &out, &out)
		if exit != 2 {
			t.Fatalf("exit mismatch: got=%d want=2", exit)
		}
	})
}
