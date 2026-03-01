package paths

import (
	"path/filepath"
	"testing"
)

func TestResolveConfiguredPath_Basic(t *testing.T) {
	home := filepath.Clean(t.TempDir())
	absoluteInput := filepath.Join(home, "nested", "..", "absolute-target")

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: ""},
		{name: "whitespace", value: "   \n\t", want: ""},
		{name: "tilde home", value: "~", want: filepath.Clean(home)},
		{name: "tilde child", value: "~/bin", want: filepath.Join(home, "bin")},
		{name: "absolute", value: absoluteInput, want: filepath.Clean(absoluteInput)},
		{name: "relative", value: "x/y", want: filepath.Join(home, "x", "y")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveConfiguredPath(home, tc.value); got != tc.want {
				t.Fatalf("ResolveConfiguredPath(%q, %q) = %q, want %q", home, tc.value, got, tc.want)
			}
		})
	}
}
