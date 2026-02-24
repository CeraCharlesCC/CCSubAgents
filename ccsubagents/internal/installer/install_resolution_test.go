package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReleaseForInstall_UsesTagEndpointWhenVersionRequested(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	latestRequests := 0
	tagRequests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case releaseLatestURL:
			latestRequests++
			body := `{"id":300,"tag_name":"v-latest","assets":[]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case releaseTagsURLPrefix + "v1.2.3":
			tagRequests++
			body := `{"id":301,"tag_name":"v1.2.3","assets":[]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		default:
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		}
	})}

	m := &Manager{
		httpClient: client,
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.installVersionRaw = "1.2.3"

	release, err := m.resolveReleaseForInstall(context.Background())
	if err != nil {
		t.Fatalf("resolveReleaseForInstall returned error: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("expected v1.2.3, got %q", release.TagName)
	}
	if tagRequests != 1 || latestRequests != 0 {
		t.Fatalf("expected tag endpoint only, got tag=%d latest=%d", tagRequests, latestRequests)
	}
}

func TestResolveReleaseForInstall_BlockedWhenPinnedAndVersionMissing(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}

	_, err := m.resolveReleaseForInstall(context.Background())
	if err == nil {
		t.Fatalf("expected pinned-version error")
	}
	if !strings.Contains(err.Error(), "install is pinned to v1.2.3") {
		t.Fatalf("expected pinned-version error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestResolveReleaseForInstall_BlockedWhenPinnedAndVersionMismatch(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.installVersionRaw = "v1.2.4"

	_, err := m.resolveReleaseForInstall(context.Background())
	if err == nil {
		t.Fatalf("expected pinned-version mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestResolveReleaseForInstall_AllowsMatchingPinnedVersion(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	tagRequests := 0
	latestRequests := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case releaseTagsURLPrefix + "v1.2.3":
				tagRequests++
				body := `{"id":401,"tag_name":"v1.2.3","assets":[]}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case releaseLatestURL:
				latestRequests++
				body := `{"id":402,"tag_name":"v-latest","assets":[]}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			default:
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.installVersionRaw = "1.2.3"

	release, err := m.resolveReleaseForInstall(context.Background())
	if err != nil {
		t.Fatalf("resolveReleaseForInstall returned error: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("expected v1.2.3, got %q", release.TagName)
	}
	if tagRequests != 1 || latestRequests != 0 {
		t.Fatalf("expected tag endpoint only, got tag=%d latest=%d", tagRequests, latestRequests)
	}
}

func TestResolveReleaseForInstall_NotFoundWarnsAndAborts(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	var status bytes.Buffer
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != releaseTagsURLPrefix+"v9.9.9" {
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`)), Header: make(http.Header)}, nil
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
		statusOut:  &status,
	}
	m.installVersionRaw = "v9.9.9"

	_, err := m.resolveReleaseForInstall(context.Background())
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	if !strings.Contains(err.Error(), "requested version v9.9.9 was not found") {
		t.Fatalf("expected requested-version error, got %v", err)
	}

	output := status.String()
	for _, want := range []string{"⚠ Requested version does not exist", "Version v9.9.9 was not found"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected warning output containing %q, got:\n%s", want, output)
		}
	}
}

func TestResolveReleaseForInstall_PinRequestedDefersPinnedVersionWrite(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "ccsubagents"), stateDirPerm); err != nil {
		t.Fatalf("create local ccsubagents directory: %v", err)
	}

	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != releaseTagsURLPrefix+"v1.2.3" {
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
			body := `{"id":501,"tag_name":"v1.2.3","assets":[]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.installVersionRaw = "1.2.3"
	m.pinRequested = true

	if _, err := m.resolveReleaseForInstall(context.Background()); err != nil {
		t.Fatalf("resolveReleaseForInstall returned error: %v", err)
	}

	_, localPath := resolveSettingsPaths(home, cwd)
	if _, err := os.Stat(localPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no settings write during release resolution, got stat err=%v", err)
	}
	if m.pendingPinWrite == nil {
		t.Fatalf("expected pending pin write to be staged")
	}
	if filepath.Clean(m.pendingPinWrite.path) != filepath.Clean(localPath) {
		t.Fatalf("expected pending pin path %q, got %q", localPath, m.pendingPinWrite.path)
	}
	if m.pendingPinWrite.versionTag != "v1.2.3" {
		t.Fatalf("expected pending version v1.2.3, got %q", m.pendingPinWrite.versionTag)
	}
}

func TestInstallOrUpdate_PinRequestedFailedInstallDoesNotPersistPinnedVersion(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "ccsubagents"), stateDirPerm); err != nil {
		t.Fatalf("create local ccsubagents directory: %v", err)
	}

	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != releaseTagsURLPrefix+"v1.2.3" {
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
			body := `{"id":502,"tag_name":"v1.2.3","assets":[]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.installVersionRaw = "1.2.3"
	m.pinRequested = true

	err := m.installOrUpdate(context.Background(), false)
	if err == nil {
		t.Fatalf("expected install failure due missing assets")
	}
	if !strings.Contains(err.Error(), "missing required asset") {
		t.Fatalf("expected missing-asset error, got %v", err)
	}

	globalPath, localPath := resolveSettingsPaths(home, cwd)
	if _, err := os.Stat(localPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected local settings to remain untouched after failed install, got stat err=%v", err)
	}
	if _, err := os.Stat(globalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected global settings to remain untouched after failed install, got stat err=%v", err)
	}
}

func TestResolveReleaseForInstall_PinRequestedWithoutVersionReturnsEarly(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}
	m.pinRequested = true

	_, err := m.resolveReleaseForInstall(context.Background())
	if !errors.Is(err, ErrPinnedRequiresVersion) {
		t.Fatalf("expected ErrPinnedRequiresVersion, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestResolveReleaseForUpdate_BlockedWhenPinnedVersionSet(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}

	_, err := m.resolveReleaseForUpdate(context.Background())
	if err == nil {
		t.Fatalf("expected pinned update-block error")
	}
	if !strings.Contains(err.Error(), "update is blocked") {
		t.Fatalf("expected update-block error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestInstallOrUpdate_UpdateBlockedWhenPinnedVersionSet(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}

	err := m.installOrUpdate(context.Background(), true)
	if err == nil {
		t.Fatalf("expected update-block error")
	}
	if !strings.Contains(err.Error(), "update is blocked") {
		t.Fatalf("expected update-block error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestInstallOrUpdateLocal_UpdateBlockedWhenPinnedVersionSet(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	globalPath, _ := resolveSettingsPaths(home, cwd)
	writeSettingsFixture(t, globalPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return cwd, nil },
	}

	err := m.installOrUpdateLocal(context.Background(), localInstallConfig{
		isUpdate: true,
		location: localScopeLocation{installRoot: t.TempDir()},
		stateDir: t.TempDir(),
		state:    &trackedState{Version: trackedSchemaVersion},
	})
	if err == nil {
		t.Fatalf("expected local update-block error")
	}
	if !strings.Contains(err.Error(), "update is blocked") {
		t.Fatalf("expected local update-block error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}

func TestInstallOrUpdateLocal_UpdateUsesSelectedInstallRootForPinResolution(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	subdir := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(subdir, stateDirPerm); err != nil {
		t.Fatalf("create nested working directory: %v", err)
	}

	_, localPath := resolveSettingsPaths(home, repoRoot)
	writeSettingsFixture(t, localPath, `{"pinned-version":"v1.2.3"}`)

	requestCount := 0
	m := &Manager{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		})},
		homeDir:    func() (string, error) { return home, nil },
		workingDir: func() (string, error) { return subdir, nil },
	}

	err := m.installOrUpdateLocal(context.Background(), localInstallConfig{
		isUpdate: true,
		location: localScopeLocation{installRoot: repoRoot, repoRoot: repoRoot, inGitRepo: true},
		stateDir: t.TempDir(),
		state:    &trackedState{Version: trackedSchemaVersion},
	})
	if err == nil {
		t.Fatalf("expected local update-block error")
	}
	if !strings.Contains(err.Error(), "update is blocked") {
		t.Fatalf("expected update-block error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no network requests, got %d", requestCount)
	}
}
