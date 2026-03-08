package files

import (
	"archive/zip"
	"bytes"
	"io"
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

func TestExtractBundleBinaries_WritesRequestedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bundle.zip")
	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"nested/mcp": "mcp-binary",
		"web":        "web-binary",
		"ignored":    "skip",
	})

	extracted, err := ExtractBundleBinaries(zipPath, destDir, []string{"mcp", "web"}, DefaultBinaryFilePerm)
	if err != nil {
		t.Fatalf("extract bundle binaries: %v", err)
	}

	for name, want := range map[string]string{"mcp": "mcp-binary", "web": "web-binary"} {
		path, ok := extracted[name]
		if !ok {
			t.Fatalf("expected extracted path for %q", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read extracted file %q: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("extracted %q = %q, want %q", name, string(data), want)
		}
	}
}

func TestWriteZipEntry_RejectsOversizedEntryAndRemovesPartialFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bundle.zip")
	destPath := filepath.Join(dir, "mcp")
	mustWriteZipFile(t, zipPath, map[string]string{
		"mcp": strings.Repeat("x", 32),
	})

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	if len(r.File) != 1 {
		t.Fatalf("expected exactly one archive entry, got %d", len(r.File))
	}

	_, err = writeZipEntry(r.File[0], destPath, DefaultBinaryFilePerm, 8)
	if err == nil {
		t.Fatalf("expected oversized entry error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size limit error, got %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected partial file removed, stat err: %v", statErr)
	}
}

func TestExtractAgentsArchiveWithHook_RejectsOversizedEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "agents.zip")
	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"agents/too-large.agent.md": strings.Repeat("a", 32),
	})

	_, _, err := extractAgentsArchiveWithHookAndLimits(zipPath, destDir, nil, DefaultStateDirPerm, DefaultStateFilePerm, 8, 64)
	if err == nil {
		t.Fatalf("expected oversized entry error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size limit error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "too-large.agent.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected oversized file not to remain on disk, stat err: %v", statErr)
	}
}

func TestExtractAgentsArchiveWithHook_RejectsArchiveOverTotalLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "agents.zip")
	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"agents/one.agent.md": strings.Repeat("a", 8),
		"agents/two.agent.md": strings.Repeat("b", 8),
	})

	_, _, err := extractAgentsArchiveWithHookAndLimits(zipPath, destDir, nil, DefaultStateDirPerm, DefaultStateFilePerm, 16, 12)
	if err == nil {
		t.Fatalf("expected total archive size error")
	}
	if !strings.Contains(err.Error(), "archive file") || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected bounded write failure once archive budget is exhausted, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "one.agent.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected extracted files removed after failure, stat err: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "two.agent.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected extracted files removed after failure, stat err: %v", statErr)
	}
}

func TestExtractAgentsArchiveWithHook_RejectsSymlinkDirectoryInDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "agents.zip")
	destDir := filepath.Join(dir, "dest")
	outsideDir := filepath.Join(dir, "outside")
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(destDir, "nested")); err != nil {
		t.Fatalf("create nested symlink: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"agents/nested/file.agent.md": "agent",
	})

	_, _, err := ExtractAgentsArchiveWithHook(zipPath, destDir, nil, DefaultStateDirPerm, DefaultStateFilePerm)
	if err == nil {
		t.Fatalf("expected symlink path rejection")
	}
	if !strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "file.agent.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected archive not to write through symlink, stat err: %v", statErr)
	}
}

func TestExtractAgentsArchiveWithHook_RejectsSymlinkFileDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "agents.zip")
	destDir := filepath.Join(dir, "dest")
	outsideFile := filepath.Join(dir, "outside.agent.md")
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}
	if err := os.WriteFile(outsideFile, []byte("outside"), DefaultStateFilePerm); err != nil {
		t.Fatalf("create outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(destDir, "agent.agent.md")); err != nil {
		t.Fatalf("create file symlink: %v", err)
	}

	mustWriteZipFile(t, zipPath, map[string]string{
		"agents/agent.agent.md": "managed",
	})

	_, _, err := ExtractAgentsArchiveWithHook(zipPath, destDir, nil, DefaultStateDirPerm, DefaultStateFilePerm)
	if err == nil {
		t.Fatalf("expected symlink path rejection")
	}
	if !strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
	data, readErr := os.ReadFile(outsideFile)
	if readErr != nil {
		t.Fatalf("read outside file: %v", readErr)
	}
	if string(data) != "outside" {
		t.Fatalf("expected outside file to remain unchanged, got %q", string(data))
	}
}

func TestInstallBinary_RejectsSymlinkDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src")
	destDir := filepath.Join(dir, "dest")
	outsideFile := filepath.Join(dir, "outside")
	destPath := filepath.Join(destDir, "binary")
	if err := os.WriteFile(srcPath, []byte("binary"), DefaultBinaryFilePerm); err != nil {
		t.Fatalf("create source file: %v", err)
	}
	if err := os.MkdirAll(destDir, DefaultStateDirPerm); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}
	if err := os.WriteFile(outsideFile, []byte("outside"), DefaultStateFilePerm); err != nil {
		t.Fatalf("create outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, destPath); err != nil {
		t.Fatalf("create destination symlink: %v", err)
	}

	err := InstallBinary(srcPath, destPath, DefaultBinaryFilePerm)
	if err == nil {
		t.Fatalf("expected symlink path rejection")
	}
	if !strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
	data, readErr := os.ReadFile(outsideFile)
	if readErr != nil {
		t.Fatalf("read outside file: %v", readErr)
	}
	if string(data) != "outside" {
		t.Fatalf("expected outside file to remain unchanged, got %q", string(data))
	}
}

func mustWriteZipFile(t *testing.T, path string, files map[string]string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), DefaultStateFilePerm); err != nil {
		t.Fatalf("write zip file: %v", err)
	}
}
