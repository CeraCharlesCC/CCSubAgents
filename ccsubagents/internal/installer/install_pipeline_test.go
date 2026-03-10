package installer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
			{Name: localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH), BrowserDownloadURL: "https://example.invalid/assets/" + localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH)},
		},
	}

	tmpDir, downloaded, err := m.downloadRequiredAssets(
		context.Background(),
		stateDir,
		release,
		[]string{assetAgentsZip, localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH)},
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

func TestDownloadRequiredAssets_FallsBackToLocalArtifactRelease(t *testing.T) {
	stateDir := t.TempDir()
	bundleAssetName := localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH)
	companionTagURL := release.TagsURLPrefix + url.PathEscape(localArtifactTagPrefix+"v1.2.3")

	companionRequests := 0
	downloadRequests := 0
	m := &Runner{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case companionTagURL:
				companionRequests++
				body := `{"id":9001,"tag_name":"local-artifact/v1.2.3","assets":[{"name":"` + bundleAssetName + `","browser_download_url":"https://example.invalid/assets/` + bundleAssetName + `"}]}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + assetAgentsZip:
				downloadRequests++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("agents")), Header: make(http.Header)}, nil
			case "https://example.invalid/assets/" + bundleAssetName:
				downloadRequests++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("bundle")), Header: make(http.Header)}, nil
			default:
				return nil, errors.New("unexpected request URL: " + req.URL.String())
			}
		})},
	}

	rel := release.Response{
		TagName: "v1.2.3",
		Assets: []release.Asset{
			{Name: assetAgentsZip, BrowserDownloadURL: "https://example.invalid/assets/" + assetAgentsZip},
		},
	}

	tmpDir, downloaded, err := m.downloadRequiredAssets(
		context.Background(),
		stateDir,
		rel,
		[]string{assetAgentsZip, bundleAssetName},
		"download-required-assets-*",
	)
	if err != nil {
		t.Fatalf("downloadRequiredAssets returned error: %v", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tmpDir); removeErr != nil {
			t.Fatalf("remove temp dir: %v", removeErr)
		}
	}()

	if companionRequests != 1 {
		t.Fatalf("expected exactly 1 companion-release request, got %d", companionRequests)
	}
	if downloadRequests != 2 {
		t.Fatalf("expected 2 download requests, got %d", downloadRequests)
	}
	if _, ok := downloaded[assetAgentsZip]; !ok {
		t.Fatalf("expected downloaded map to include %q", assetAgentsZip)
	}
	bundlePath, ok := downloaded[bundleAssetName]
	if !ok {
		t.Fatalf("expected downloaded map to include %q", bundleAssetName)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("expected downloaded bundle file to exist, got %v", err)
	}
}

func TestDownloadRequiredAssets_CompanionMissingStillReturnsMissingAsset(t *testing.T) {
	stateDir := t.TempDir()
	bundleAssetName := localArtifactBundleAssetName(runtime.GOOS, runtime.GOARCH)
	companionTagURL := release.TagsURLPrefix + url.PathEscape(localArtifactTagPrefix+"v1.2.3")

	companionRequests := 0
	m := &Runner{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case companionTagURL:
				companionRequests++
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
					Header:     make(http.Header),
				}, nil
			default:
				return nil, errors.New("unexpected request URL: " + req.URL.String())
			}
		})},
	}

	rel := release.Response{
		TagName: "v1.2.3",
		Assets: []release.Asset{
			{Name: assetAgentsZip, BrowserDownloadURL: "https://example.invalid/assets/" + assetAgentsZip},
		},
	}

	_, _, err := m.downloadRequiredAssets(
		context.Background(),
		stateDir,
		rel,
		[]string{assetAgentsZip, bundleAssetName},
		"download-required-assets-*",
	)
	if err == nil {
		t.Fatalf("expected missing-asset error")
	}
	if !strings.Contains(err.Error(), "missing required asset \""+bundleAssetName+"\"") {
		t.Fatalf("expected missing-asset error, got %v", err)
	}
	if companionRequests != 1 {
		t.Fatalf("expected exactly 1 companion-release request, got %d", companionRequests)
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
		"attestation verification failed for agents.zip",
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
	mcpBinaryName, webBinaryName := localArtifactBinaryNames(runtime.GOOS)
	bundleBinaries := map[string]string{
		mcpBinaryName: filepath.Join(bundleDir, mcpBinaryName),
		webBinaryName: filepath.Join(bundleDir, webBinaryName),
	}

	m := &Runner{
		installBinary: func(string, string) error {
			return os.ErrPermission
		},
	}

	mutations := files.NewMutationTracker(files.NewRollback(), stateDirPerm)
	_, err := m.installExtractedBinaries(context.Background(), bundleBinaries, destinationDir, mutations, destinationDir, runtime.GOOS)
	if err == nil {
		t.Fatalf("expected installExtractedBinaries to fail")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected wrapped permission error, got %v", err)
	}
	if !strings.Contains(err.Error(), "requires privileges to write "+destinationDir) {
		t.Fatalf("expected privilege hint in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "install "+mcpBinaryName+" into "+destinationDir) {
		t.Fatalf("expected wrapped install context in error, got %v", err)
	}
}
