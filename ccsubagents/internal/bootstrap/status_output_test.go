package bootstrap

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
	"time"
)

func assertStatusContainsInOrder(t *testing.T, status string, wants []string) {
	t.Helper()

	cursor := 0
	for _, want := range wants {
		idx := strings.Index(status[cursor:], want)
		if idx < 0 {
			t.Fatalf("expected status output to contain %q after position %d, got:\n%s", want, cursor, status)
		}
		cursor += idx + len(want)
	}
}

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
		getenv: func(key string) string {
			if key != "PATH" {
				return ""
			}
			return strings.Join([]string{"/usr/bin", filepath.Join(home, binaryInstallDirDefaultRel), "/bin"}, string(os.PathListSeparator))
		},
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
	assertStatusContainsInOrder(t, status, []string{
		"ccsubagents v1.2.3",
		"✓ Checked for existing installation (none found)",
		"✓ Downloaded release assets (v1.2.3)",
		"✓ Verified attestations",
		"✓ Installed binaries (→ ~/.local/bin)",
		"✓ Installed agent definitions (→ ~/.local/share/ccsubagents/agents)",
		"✓ Updated VS Code settings and MCP config",
		"Install complete.",
	})

	for _, disallowed := range []string{"==>", "  - ", "local-artifact-mcp", "local-artifact-web", "agents.zip", "local-artifact.zip"} {
		if strings.Contains(status, disallowed) {
			t.Fatalf("expected compact default status without %q, got:\n%s", disallowed, status)
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
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
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
	assertStatusContainsInOrder(t, status, []string{
		"ccsubagents v2.0.0",
		"✓ Checked for existing installation (v1.9.9 found)",
		"✓ Downloaded release assets (v2.0.0)",
		"✓ Verified attestations",
		"✓ Installed binaries (→ ~/.local/bin)",
		"✓ Installed agent definitions (→ ~/.local/share/ccsubagents/agents)",
		"✓ Updated VS Code settings and MCP config",
		"✓ Removed stale managed agent files",
		"Update complete.",
	})
}

func TestInstallOrUpdate_ReportsSkipAttestationWhenFlagSet(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
	})

	var out bytes.Buffer
	m := statusTestManager(home, successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive), &out)
	m.skipAttestationsCheck = true

	if err := m.installOrUpdate(context.Background(), false); err != nil {
		t.Fatalf("install should succeed: %v", err)
	}

	status := out.String()
	assertStatusContainsInOrder(t, status, []string{
		"✓ Verified attestations (skipped (--skip-attestations-check))",
	})
	if strings.Contains(status, "verified attestation:") {
		t.Fatalf("expected no per-asset attestation details in compact mode, got:\n%s", status)
	}
}

func TestInstallOrUpdate_ReportsAlreadyLatestNoopForUpdate(t *testing.T) {
	home := t.TempDir()
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if req.URL.String() != releaseLatestURL {
			return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
		}
		body := `{"id":101,"tag_name":"v2.0.0","assets":[]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	var out bytes.Buffer
	m := statusTestManager(home, client, &out)

	stateDir := filepath.Join(home, ".local", "share", "ccsubagents")
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	if err := m.saveTrackedState(stateDir, trackedState{
		Version:     trackedSchemaVersion,
		Repo:        releaseRepo,
		ReleaseID:   100,
		ReleaseTag:  "v2.0.0",
		InstalledAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed tracked state: %v", err)
	}

	if err := m.installOrUpdate(context.Background(), true); err != nil {
		t.Fatalf("update no-op should succeed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected only release metadata request on no-op update, got %d", requestCount)
	}

	want := "ccsubagents: already at latest version (v2.0.0). Nothing to do.\n"
	if out.String() != want {
		t.Fatalf("expected exact no-op update message %q, got %q", want, out.String())
	}
}

func TestInstallOrUpdate_VerboseDetailGating(t *testing.T) {
	runInstall := func(t *testing.T, verbose bool) string {
		t.Helper()
		home := t.TempDir()
		agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
		bundleArchive := zipBytes(t, map[string]string{
			"local-artifact-mcp": "mcp-binary",
			"local-artifact-web": "web-binary",
		})

		var out bytes.Buffer
		m := statusTestManager(home, successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive), &out)
		m.verbose = verbose

		if err := m.installOrUpdate(context.Background(), false); err != nil {
			t.Fatalf("install should succeed: %v", err)
		}
		return out.String()
	}

	compact := runInstall(t, false)
	verbose := runInstall(t, true)

	for _, detail := range []string{"downloaded agents.zip", "verified attestation:", "installed binary:", "updated settings:", "updated mcp config:"} {
		if strings.Contains(compact, detail) {
			t.Fatalf("expected compact output to omit detail %q, got:\n%s", detail, compact)
		}
		if !strings.Contains(verbose, detail) {
			t.Fatalf("expected verbose output to include detail %q, got:\n%s", detail, verbose)
		}
	}
}

func TestInstallOrUpdate_AttestationFailureReportsActionableGuidance(t *testing.T) {
	home := t.TempDir()
	agentsArchive := zipBytes(t, map[string]string{"agents/example.agent.md": "content"})
	bundleArchive := zipBytes(t, map[string]string{
		"local-artifact-mcp": "mcp-binary",
		"local-artifact-web": "web-binary",
	})

	var out bytes.Buffer
	m := statusTestManager(home, successReleaseHTTPClient(t, "v1.2.3", agentsArchive, bundleArchive), &out)
	m.runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("verification failed")
	}

	err := m.installOrUpdate(context.Background(), false)
	if err == nil {
		t.Fatalf("expected attestation verification error")
	}
	for _, want := range []string{
		"Error: attestation verification failed for agents.zip",
		"To skip verification: ccsubagents install --skip-attestations-check",
		"(not recommended for production use)",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to include %q, got %v", want, err)
		}
	}

	status := out.String()
	assertStatusContainsInOrder(t, status, []string{
		"ccsubagents v1.2.3",
		"✓ Checked for existing installation (none found)",
		"✓ Downloaded release assets (v1.2.3)",
		"✗ Verified attestations",
		"Failed asset: agents.zip",
	})
}

func TestUninstall_ReportsNoopWhenNoTrackedState(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	m := statusTestManager(home, &http.Client{}, &out)

	if err := m.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall should be a no-op when tracked state is missing: %v", err)
	}

	status := out.String()
	assertStatusContainsInOrder(t, status, []string{
		"✓ Checked tracked installation state (none found)",
	})
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
	assertStatusContainsInOrder(t, status, []string{
		"✓ Checked tracked installation state (v1.0.0 found)",
		"✓ Removed managed files",
		"✓ Reverted settings and MCP configuration",
		"✓ Removed managed directories",
		"✓ Updated tracked installation state (removed)",
		"Uninstall complete.",
	})
}

func TestReportGlobalPathWarning_ContainsHeadlineAndGuidanceLines(t *testing.T) {
	home := t.TempDir()

	var out bytes.Buffer
	m := statusTestManager(home, &http.Client{}, &out)
	m.getenv = func(key string) string {
		if key != "PATH" {
			return ""
		}
		return strings.Join([]string{"/usr/bin", "/bin"}, string(os.PathListSeparator))
	}

	m.reportGlobalPathWarning(home)
	status := out.String()
	for _, want := range []string{
		"⚠ ~/.local/bin is not in PATH",
		"Add it to your shell profile:",
		"export PATH=\"$HOME/.local/bin:$PATH\"",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected PATH warning output to contain %q, got %q", want, status)
		}
	}
}

func TestReportGlobalPathWarning_NoWarningWhenPathContainsLocalBin(t *testing.T) {
	home := t.TempDir()
	expected := filepath.Join(home, binaryInstallDirDefaultRel)

	var out bytes.Buffer
	m := statusTestManager(home, &http.Client{}, &out)
	m.getenv = func(key string) string {
		if key != "PATH" {
			return ""
		}
		return strings.Join([]string{"/usr/bin", expected, "/bin"}, string(os.PathListSeparator))
	}

	m.reportGlobalPathWarning(home)
	if got := out.String(); got != "" {
		t.Fatalf("expected no PATH warning when local bin is present, got %q", got)
	}
}
