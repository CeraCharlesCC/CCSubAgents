package bootstrap

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Command
		wantErr string
	}{
		{name: "install", input: "install", want: CommandInstall},
		{name: "update", input: "update", want: CommandUpdate},
		{name: "uninstall", input: "uninstall", want: CommandUninstall},
		{name: "trimmed", input: "  install  ", want: CommandInstall},
		{name: "invalid", input: "upgrade", wantErr: "unknown command"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCommand(tc.input)
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
				t.Fatalf("ParseCommand returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestResolveScope(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		tests := []struct {
			name    string
			command Command
			want    Scope
		}{
			{name: "install defaults local", command: CommandInstall, want: ScopeLocal},
			{name: "update defaults global", command: CommandUpdate, want: ScopeGlobal},
			{name: "uninstall defaults global", command: CommandUninstall, want: ScopeGlobal},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := ResolveScope(tc.command, "")
				if err != nil {
					t.Fatalf("ResolveScope returned error: %v", err)
				}
				if got != tc.want {
					t.Fatalf("expected %q, got %q", tc.want, got)
				}
			})
		}
	})

	t.Run("explicit override", func(t *testing.T) {
		got, err := ResolveScope(CommandInstall, " global ")
		if err != nil {
			t.Fatalf("ResolveScope returned error: %v", err)
		}
		if got != ScopeGlobal {
			t.Fatalf("expected %q, got %q", ScopeGlobal, got)
		}
	})

	t.Run("invalid scope", func(t *testing.T) {
		_, err := ResolveScope(CommandInstall, "team")
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "unknown scope") {
			t.Fatalf("expected unknown scope error, got %q", err.Error())
		}
	})
}
