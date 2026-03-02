package mcp

import (
	"strings"
	"testing"
)

func TestResolveWorkspaceHashOverrideFromEnv(t *testing.T) {
	valid := strings.Repeat("a", 64)

	cases := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{name: "unset", env: "", want: "", wantErr: false},
		{name: "valid lowercase", env: valid, want: valid, wantErr: false},
		{name: "valid uppercase is normalized", env: strings.ToUpper(valid), want: valid, wantErr: false},
		{name: "invalid length", env: strings.Repeat("b", 63), wantErr: true},
		{name: "invalid hex", env: strings.Repeat("z", 64), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key != workspaceHashOverrideEnv {
					return ""
				}
				return tc.env
			}
			got, err := resolveWorkspaceHashOverrideFromEnv(getenv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("override mismatch: got=%q want=%q", got, tc.want)
			}
		})
	}
}
