package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

type releaseResponse struct {
	ID      int64          `json:"id"`
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func mapRequiredAssets(assets []releaseAsset, names []string) (map[string]releaseAsset, error) {
	byName := make(map[string]releaseAsset, len(assets))
	for _, asset := range assets {
		byName[asset.Name] = asset
	}
	out := make(map[string]releaseAsset, len(names))
	for _, name := range names {
		asset, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("latest release is missing required asset %q", name)
		}
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			return nil, fmt.Errorf("release asset %q has no download URL", name)
		}
		out[name] = asset
	}
	return out, nil
}

func (m *Manager) fetchLatestRelease(ctx context.Context) (releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseLatestURL, nil)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("create latest release request: %w", err)
	}
	req.Header.Set("Accept", httpsHeaderAccept)
	req.Header.Set("User-Agent", httpsHeaderUserAgent)
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(httpsHeaderAuthorization, httpsHeaderGithubTokenPref+token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("request latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return releaseResponse{}, fmt.Errorf("latest release request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return releaseResponse{}, fmt.Errorf("decode latest release response: %w", err)
	}
	if strings.TrimSpace(decoded.TagName) == "" {
		return releaseResponse{}, errors.New("latest release response is missing tag_name")
	}
	return decoded, nil
}

func (m *Manager) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", httpsHeaderUserAgent)
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(httpsHeaderAuthorization, httpsHeaderGithubTokenPref+token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stateFilePerm)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("copy response to destination: %w", err)
	}
	return nil
}

func (m *Manager) verifyDownloadedAssets(ctx context.Context, downloaded map[string]string) error {
	if _, err := m.lookPath("gh"); err != nil {
		return errors.New("gh CLI is required for attestation verification but was not found in PATH")
	}

	names := make([]string, 0, len(downloaded))
	for name := range downloaded {
		names = append(names, name)
	}
	sort.Strings(names)

	certIdentity := "https://github.com/" + releaseRepo + "/" + releaseWorkflowPath + "@refs/heads/main"
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.reportAction("Verifying attestation for %s", name)
		path := downloaded[name]
		_, err := m.runCommand(
			ctx,
			"gh",
			"attestation",
			"verify",
			path,
			"--repo",
			releaseRepo,
			"--cert-identity",
			certIdentity,
			"--cert-oidc-issuer",
			attestationOIDCIssuer,
		)
		if err != nil {
			return fmt.Errorf("attestation verification failed for %s: %w", name, err)
		}
	}
	return nil
}
