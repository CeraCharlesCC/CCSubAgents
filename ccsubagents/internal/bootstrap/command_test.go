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
