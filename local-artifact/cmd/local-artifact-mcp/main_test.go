package main

import (
	"path/filepath"
	"testing"
)

func TestLocalArtifactWebPath(t *testing.T) {
	base := filepath.Join("tmp", "bin")

	tests := []struct {
		name    string
		goos    string
		exePath string
		want    string
	}{
		{
			name:    "linux",
			goos:    "linux",
			exePath: filepath.Join(base, "local-artifact-mcp"),
			want:    filepath.Join(base, "local-artifact-web"),
		},
		{
			name:    "darwin",
			goos:    "darwin",
			exePath: filepath.Join(base, "local-artifact-mcp"),
			want:    filepath.Join(base, "local-artifact-web"),
		},
		{
			name:    "windows",
			goos:    "windows",
			exePath: filepath.Join(base, "local-artifact-mcp.exe"),
			want:    filepath.Join(base, "local-artifact-web.exe"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := localArtifactWebPath(tc.exePath, tc.goos)
			if got != tc.want {
				t.Fatalf("localArtifactWebPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
