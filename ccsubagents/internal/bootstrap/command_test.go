package bootstrap

import (
	"strings"
	"testing"
)

func requireErrContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got %v", substr, err)
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  Command
		err   string
	}{
		{input: "install", want: CommandInstall},
		{input: "update", want: CommandUpdate},
		{input: "uninstall", want: CommandUninstall},
		{input: "  install  ", want: CommandInstall},
		{input: "upgrade", err: "unknown command"},
	}

	for i, tc := range tests {
		got, err := ParseCommand(tc.input)
		if tc.err != "" {
			requireErrContains(t, err, tc.err)
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("case %d: got %q/%v want %q/<nil>", i, got, err, tc.want)
		}
	}
}

func TestResolveScope(t *testing.T) {
	tests := []struct {
		command Command
		raw     string
		want    Scope
		err     string
	}{
		{command: CommandInstall, want: ScopeLocal},
		{command: CommandUpdate, want: ScopeGlobal},
		{command: CommandUninstall, want: ScopeGlobal},
		{command: CommandInstall, raw: " global ", want: ScopeGlobal},
		{command: CommandInstall, raw: "team", err: "unknown scope"},
	}

	for i, tc := range tests {
		got, err := ResolveScope(tc.command, tc.raw)
		if tc.err != "" {
			requireErrContains(t, err, tc.err)
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("case %d: got %q/%v want %q/<nil>", i, got, err, tc.want)
		}
	}
}
