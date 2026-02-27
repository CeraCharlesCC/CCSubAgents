package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultGlobalBaseForGOOS(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")

	tests := []struct {
		goos string
		want string
	}{
		{goos: "linux", want: filepath.Join(home, ".local", "share", "ccsubagents")},
		{goos: "darwin", want: filepath.Join(home, "Library", "Application Support", "ccsubagents")},
		{goos: "windows", want: filepath.Join(home, "AppData", "Local", "ccsubagents")},
	}

	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			got := defaultGlobalBaseForGOOS(tc.goos, home)
			if got != tc.want {
				t.Fatalf("base mismatch: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestResolveStateDir_FallsBackToCCSubagentsOverride(t *testing.T) {
	t.Setenv(stateDirEnv, "")
	want := filepath.Join(t.TempDir(), "shared-state")
	t.Setenv(ccStateDirEnv, want)

	got, err := ResolveStateDir()
	if err != nil {
		t.Fatalf("ResolveStateDir failed: %v", err)
	}
	if got != want {
		t.Fatalf("state override mismatch: got=%q want=%q", got, want)
	}
}

func TestResolveStateDir_DefaultMatchesPlatformParityLayout(t *testing.T) {
	home := t.TempDir()
	setTestHomeEnv(t, home)
	t.Setenv(stateDirEnv, "")
	t.Setenv(ccStateDirEnv, "")

	got, err := ResolveStateDir()
	if err != nil {
		t.Fatalf("ResolveStateDir failed: %v", err)
	}
	want := filepath.Join(defaultGlobalBaseForGOOS(runtime.GOOS, home), "state")
	if got != want {
		t.Fatalf("default state mismatch: got=%q want=%q", got, want)
	}
}

func setTestHomeEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS != "windows" {
		return
	}

	volume := filepath.VolumeName(home)
	if volume == "" {
		volume = os.Getenv("HOMEDRIVE")
	}
	t.Setenv("HOMEDRIVE", volume)

	homePath := strings.TrimPrefix(home, volume)
	if homePath == "" {
		homePath = string(os.PathSeparator)
	}
	t.Setenv("HOMEPATH", homePath)
}
