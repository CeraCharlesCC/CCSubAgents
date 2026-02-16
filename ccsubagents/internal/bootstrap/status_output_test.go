package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func successReleaseHTTPClient(t *testing.T, releaseTag string, agentsArchive, bundleArchive []byte) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		switch req.URL.String() {
		case releaseLatestURL:
			body := fmt.Sprintf(`{"id":101,"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"https://example.invalid/assets/%s"},{"name":%q,"browser_download_url":"https://example.invalid/assets/%s"}]}`,
				releaseTag,
				assetAgentsZip, assetAgentsZip,
				assetLocalArtifactZip, assetLocalArtifactZip,
			)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetAgentsZip:
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(agentsArchive)), Header: make(http.Header)}, nil
		case "https://example.invalid/assets/" + assetLocalArtifactZip:
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(bundleArchive)), Header: make(http.Header)}, nil
		default:
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		}
	})}
}

func statusTestManager(home string, client *http.Client, statusOut io.Writer) *Manager {
	return &Manager{
		httpClient: client,
		now:        func() time.Time { return time.Unix(0, 0).UTC() },
		homeDir:    func() (string, error) { return home, nil },
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		runCommand: func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil },
		statusOut:  statusOut,
	}
}

func TestInstallOrUpdate_ReportsInstallProgress(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
	})

	var out bytes.Buffer
	m := statusTestManager(home, successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive), &out)

	if err := m.installOrUpdate(context.Background(), false); err != nil {
		t.Fatalf("install should succeed: %v", err)
	}

	status := out.String()
	for _, want := range []string{
		"==> Install: resolving environment",
		"==> Install: fetching latest release metadata",
		"  - Downloading agents.zip",
		"  - Verifying attestation for agents.zip",
		"==> Install: finalizing installation state",
		"  - Install complete: v1.2.3",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected status output to contain %q, got:\n%s", want, status)
		}
	}
}

func TestInstallOrUpdate_ReportsUpdateCleanupProgress(t *testing.T) {
	home := t.TempDir()
	agentsDir := filepath.Join(home, agentsRelativePath)
	if err := os.MkdirAll(agentsDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	staleFile := filepath.Join(agentsDir, "stale.agent.md")
	if err := os.WriteFile(staleFile, []byte("stale"), stateFilePerm); err != nil {
		t.Fatalf("seed stale managed file: %v", err)
	}

	var out bytes.Buffer
	m := statusTestManager(home, successReleaseHTTPClient(t, "v2.0.0", zipBytes(t, map[string]string{"agents/new.agent.md": "fresh"}), zipBytes(t, map[string]string{
		"bundle/local-artifact-mcp": "mcp-binary",
		"bundle/local-artifact-web": "web-binary",
	})), &out)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	previous := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   100,
		ReleaseTag:  "v1.9.9",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed: managedState{
			Files: []string{staleFile},
		},
	}
	if err := m.saveTrackedState(stateDir, previous); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.installOrUpdate(context.Background(), true); err != nil {
		t.Fatalf("update should succeed: %v", err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("expected stale managed file removed, stat err: %v", err)
	}

	status := out.String()
	for _, want := range []string{
		"==> Update: resolving environment",
		"==> Update: cleaning up stale managed agent files",
		"  - Removing stale managed agent files",
		"  - Update complete: v2.0.0",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected status output to contain %q, got:\n%s", want, status)
		}
	}
}

func TestUninstall_ReportsNoopWhenNoTrackedState(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	m := statusTestManager(home, &http.Client{}, &out)

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should be a no-op when tracked state is missing: %v", err)
	}

	status := out.String()
	for _, want := range []string{
		"==> Uninstall: resolving environment",
		"  - Loading tracked installation state",
		"  - No tracked install found (nothing to uninstall)",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected status output to contain %q, got:\n%s", want, status)
		}
	}
}

func TestUninstall_ReportsProgressOnTrackedState(t *testing.T) {
	home := t.TempDir()
	agentsDir := filepath.Join(home, agentsRelativePath)
	if err := os.MkdirAll(agentsDir, stateDirPerm); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	agentFile := filepath.Join(agentsDir, "example.agent.md")
	if err := os.WriteFile(agentFile, []byte("content"), stateFilePerm); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}

	var out bytes.Buffer
	m := statusTestManager(home, &http.Client{}, &out)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	state := trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   42,
		ReleaseTag:  "v1.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
		Managed: managedState{
			Files: []string{agentFile},
			Dirs:  []string{agentsDir},
		},
	}
	if err := m.saveTrackedState(stateDir, state); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should succeed: %v", err)
	}

	status := out.String()
	for _, want := range []string{
		"==> Uninstall: removing managed files",
		"==> Uninstall: reverting configuration edits",
		"==> Uninstall: finalizing",
		"  - Uninstall complete",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected status output to contain %q, got:\n%s", want, status)
		}
	}
}
