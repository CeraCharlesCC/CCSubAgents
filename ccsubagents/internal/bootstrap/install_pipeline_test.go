package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadRequiredAssets_RemovesTempDirOnDownloadError(t *testing.T) {
	stateDir := t.TempDir()

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("download failed")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	release := releaseResponse{
		TagName: "v1.2.3",
		Assets: []releaseAsset{
			{Name: assetAgentsZip, BrowserDownloadURL: "https://example.invalid/assets/" + assetAgentsZip},
			{Name: assetLocalArtifactZip, BrowserDownloadURL: "https://example.invalid/assets/" + assetLocalArtifactZip},
		},
	}

	tmpDir, downloaded, err := m.downloadRequiredAssets(
		context.Background(),
		stateDir,
		release,
		[]string{assetAgentsZip, assetLocalArtifactZip},
		"download-required-assets-*",
	)
	if err == nil {
		t.Fatalf("expected downloadRequiredAssets to fail")
	}
	if !strings.Contains(err.Error(), "download release asset \"agents.zip\"") {
		t.Fatalf("expected wrapped asset download error, got %v", err)
	}
	if tmpDir != "" {
		t.Fatalf("expected empty tmp dir on error, got %q", tmpDir)
	}
	if downloaded != nil {
		t.Fatalf("expected nil downloaded map on error, got %#v", downloaded)
	}
	if requestCount != 1 {
		t.Fatalf("expected download loop to stop after first failure, got %d requests", requestCount)
	}

	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temporary download directory to be removed, found %d entries", len(entries))
	}
}

func TestVerifyAttestationsOrReport_AttestationFailureAddsSkipGuidance(t *testing.T) {
	var status bytes.Buffer
	m := &Manager{
		statusOut: &status,
		lookPath: func(string) (string, error) {
			return "/usr/bin/gh", nil
		},
		runCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("verification failed")
		},
	}

	err := m.verifyAttestationsOrReport(
		context.Background(),
		map[string]string{assetAgentsZip: filepath.Join(t.TempDir(), assetAgentsZip)},
		false,
		ScopeLocal,
	)
	if err == nil {
		t.Fatalf("expected attestation verification to fail")
	}

	for _, want := range []string{
		"Error: attestation verification failed for agents.zip",
		"To skip verification: ccsubagents install --scope=local --skip-attestations-check",
		"(not recommended for production use)",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to include %q, got %v", want, err)
		}
	}

	statusText := status.String()
	for _, want := range []string{"✗ Verified attestations", "Failed asset: agents.zip"} {
		if !strings.Contains(statusText, want) {
			t.Fatalf("expected status output to include %q, got %q", want, statusText)
		}
	}
}

func TestInstallExtractedBinaries_PermissionErrorIncludesPrivilegeHint(t *testing.T) {
	destinationDir := t.TempDir()
	bundleDir := t.TempDir()
	bundleBinaries := map[string]string{
		assetArtifactMCP: filepath.Join(bundleDir, assetArtifactMCP),
		assetArtifactWeb: filepath.Join(bundleDir, assetArtifactWeb),
	}

	m := &Manager{
		installBinary: func(string, string) error {
			return os.ErrPermission
		},
	}

	mutations := newMutationTracker(newInstallRollback())
	_, err := m.installExtractedBinaries(context.Background(), bundleBinaries, destinationDir, mutations, destinationDir)
	if err == nil {
		t.Fatalf("expected installExtractedBinaries to fail")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected wrapped permission error, got %v", err)
	}
	if !strings.Contains(err.Error(), "requires privileges to write "+destinationDir) {
		t.Fatalf("expected privilege hint in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "install "+assetArtifactMCP+" into "+destinationDir) {
		t.Fatalf("expected wrapped install context in error, got %v", err)
	}
}

func TestExtractDownloadedBundle_UsesPlatformSpecificBundleNames(t *testing.T) {
	stateDir := t.TempDir()
	tmpDir := t.TempDir()
	bundlePath := filepath.Join(stateDir, assetLocalArtifactZip)

	if err := os.WriteFile(bundlePath, zipBytes(t, map[string]string{
		bundleArchiveBinaryName(assetArtifactMCP, "linux", "amd64"): "mcp-linux-amd64",
		bundleArchiveBinaryName(assetArtifactWeb, "linux", "amd64"): "web-linux-amd64",
	}), stateFilePerm); err != nil {
		t.Fatalf("write bundle archive: %v", err)
	}

	m := &Manager{goos: "linux", goarch: "amd64"}
	binaries, err := m.extractDownloadedBundle(tmpDir, map[string]string{assetLocalArtifactZip: bundlePath})
	if err != nil {
		t.Fatalf("extractDownloadedBundle returned error: %v", err)
	}

	mcpPath := binaries[assetArtifactMCP]
	webPath := binaries[assetArtifactWeb]
	if mcpPath == "" || webPath == "" {
		t.Fatalf("expected extracted binary paths for mcp and web, got %#v", binaries)
	}

	mcpBytes, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read extracted mcp binary: %v", err)
	}
	if string(mcpBytes) != "mcp-linux-amd64" {
		t.Fatalf("unexpected extracted mcp binary content: %q", string(mcpBytes))
	}

	webBytes, err := os.ReadFile(webPath)
	if err != nil {
		t.Fatalf("read extracted web binary: %v", err)
	}
	if string(webBytes) != "web-linux-amd64" {
		t.Fatalf("unexpected extracted web binary content: %q", string(webBytes))
	}
}

func TestInstallExtractedBinaries_UsesExeNamesOnWindows(t *testing.T) {
	destinationDir := t.TempDir()
	bundleDir := t.TempDir()

	mcpSrc := filepath.Join(bundleDir, "src-mcp")
	webSrc := filepath.Join(bundleDir, "src-web")
	if err := os.WriteFile(mcpSrc, []byte("mcp"), stateFilePerm); err != nil {
		t.Fatalf("write mcp source: %v", err)
	}
	if err := os.WriteFile(webSrc, []byte("web"), stateFilePerm); err != nil {
		t.Fatalf("write web source: %v", err)
	}

	m := &Manager{
		goos:   "windows",
		goarch: "amd64",
		installBinary: func(src, dst string) error {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, binaryFilePerm)
		},
	}

	mutations := newMutationTracker(newInstallRollback())
	paths, err := m.installExtractedBinaries(context.Background(), map[string]string{
		assetArtifactMCP: mcpSrc,
		assetArtifactWeb: webSrc,
	}, destinationDir, mutations, destinationDir)
	if err != nil {
		t.Fatalf("installExtractedBinaries returned error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 installed binary paths, got %d", len(paths))
	}
	if filepath.Base(paths[0]) != assetArtifactMCP+".exe" && filepath.Base(paths[1]) != assetArtifactMCP+".exe" {
		t.Fatalf("expected mcp .exe binary path, got %#v", paths)
	}
	if filepath.Base(paths[0]) != assetArtifactWeb+".exe" && filepath.Base(paths[1]) != assetArtifactWeb+".exe" {
		t.Fatalf("expected web .exe binary path, got %#v", paths)
	}
}
