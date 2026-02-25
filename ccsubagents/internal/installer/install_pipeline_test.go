package installer

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

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
)

func TestDownloadRequiredAssets_RemovesTempDirOnDownloadError(t *testing.T) {
	stateDir := t.TempDir()

	requestCount := 0
	m := &Runner{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("download failed")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	release := release.Response{
		TagName: "v1.2.3",
		Assets: []release.Asset{
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
	m := &Runner{
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

	m := &Runner{
		installBinary: func(string, string) error {
			return os.ErrPermission
		},
	}

	mutations := files.NewMutationTracker(files.NewRollback(), stateDirPerm)
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
