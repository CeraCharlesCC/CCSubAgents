package files

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestExtractBundleBinaries_MaxSizeLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "bundle.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(zipFile)

	entry, err := zw.Create("ccsubagents-mcp")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	chunk := strings.Repeat("a", 1<<20)
	remaining := int64(maxBundleBinarySize + 1)
	for remaining > 0 {
		toWrite := int64(len(chunk))
		if remaining < toWrite {
			toWrite = remaining
		}
		if _, err := entry.Write([]byte(chunk[:toWrite])); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
		remaining -= toWrite
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	destDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}

	_, err = ExtractBundleBinaries(zipPath, destDir, []string{"ccsubagents-mcp"}, 0o755)
	if err == nil {
		t.Fatalf("expected oversized archive entry error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected max size error, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(destDir, "ccsubagents-mcp")); !os.IsNotExist(statErr) {
		t.Fatalf("expected extracted file cleanup on error, stat err=%v", statErr)
	}
}
