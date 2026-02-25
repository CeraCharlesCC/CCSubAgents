package files

import "testing"

func TestHasWindowsDrivePrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "uppercase drive", path: "C:/agents/file.txt", want: true},
		{name: "lowercase drive", path: "c:/agents/file.txt", want: true},
		{name: "letter with relative drive path", path: "Z:agents/file.txt", want: true},
		{name: "numeric prefix", path: "1:/agents/file.txt", want: false},
		{name: "punctuation prefix", path: ":/agents/file.txt", want: false},
		{name: "absolute unix path", path: "/agents/file.txt", want: false},
		{name: "empty path", path: "", want: false},
		{name: "single letter", path: "C", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasWindowsDrivePrefix(tc.path); got != tc.want {
				t.Fatalf("hasWindowsDrivePrefix(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestCleanZipPath_WindowsDrivePrefixHandling(t *testing.T) {
	t.Parallel()

	if _, err := cleanZipPath(`C:\agents\file.txt`); err == nil {
		t.Fatalf("expected Windows drive prefix to be rejected")
	}

	got, err := cleanZipPath("1:/agents/file.txt")
	if err != nil {
		t.Fatalf("unexpected error for non-drive colon prefix: %v", err)
	}
	if got != "1:/agents/file.txt" {
		t.Fatalf("expected cleaned path 1:/agents/file.txt, got %q", got)
	}
}
