package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	Repo                  = "CeraCharlesCC/CCSubAgents"
	WorkflowPath          = ".github/workflows/manual-release.yml"
	LatestURL             = "https://api.github.com/repos/" + Repo + "/releases/latest"
	ReleasesURL           = "https://api.github.com/repos/" + Repo + "/releases?per_page=100"
	TagsURLPrefix         = "https://api.github.com/repos/" + Repo + "/releases/tags/"
	HeaderAccept          = "application/vnd.github+json"
	HeaderUserAgent       = "ccsubagents-bootstrap"
	HeaderAuthorization   = "Authorization"
	HeaderGithubTokenPref = "Bearer "
	AttestationOIDCIssuer = "https://token.actions.githubusercontent.com"
)

type Response struct {
	ID      int64   `json:"id"`
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type listResponse struct {
	ID         int64   `json:"id"`
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type AttestationVerificationError struct {
	Asset string
	Err   error
}

var ErrReleaseNotFound = errors.New("release not found")

type ReleaseNotFoundError struct {
	Tag string
}

func (e *ReleaseNotFoundError) Error() string {
	tag := strings.TrimSpace(e.Tag)
	if tag == "" {
		return "release not found"
	}
	return fmt.Sprintf("release %s not found", tag)
}

func (e *ReleaseNotFoundError) Unwrap() error {
	return ErrReleaseNotFound
}

func (e *AttestationVerificationError) Error() string {
	if e == nil {
		return "attestation verification failed"
	}
	if strings.TrimSpace(e.Asset) == "" {
		if e.Err == nil {
			return "attestation verification failed"
		}
		return fmt.Sprintf("attestation verification failed: %v", e.Err)
	}
	if e.Err == nil {
		return fmt.Sprintf("attestation verification failed for %s", e.Asset)
	}
	return fmt.Sprintf("attestation verification failed for %s: %v", e.Asset, e.Err)
}

func (e *AttestationVerificationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Client struct {
	HTTPClient *http.Client
	LookPath   func(string) (string, error)
	RunCommand func(context.Context, string, ...string) ([]byte, error)
	Getenv     func(string) string
}

func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		LookPath:   nil,
		RunCommand: nil,
		Getenv:     os.Getenv,
	}
}

func NormalizeVersionTag(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "none") {
		return ""
	}
	if strings.EqualFold(trimmed, "null") {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "V") {
		return "v" + strings.TrimPrefix(trimmed, "V")
	}
	return "v" + trimmed
}

func MapRequiredAssets(assets []Asset, names []string) (map[string]Asset, error) {
	byName := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		byName[asset.Name] = asset
	}
	out := make(map[string]Asset, len(names))
	for _, name := range names {
		asset, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("release is missing required asset %q", name)
		}
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			return nil, fmt.Errorf("release asset %q has no download URL", name)
		}
		out[name] = asset
	}
	return out, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) getenv(key string) string {
	if c.Getenv != nil {
		return c.Getenv(key)
	}
	return os.Getenv(key)
}

func (c *Client) FetchLatest(ctx context.Context) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesURL, nil)
	if err != nil {
		return Response{}, fmt.Errorf("create releases request: %w", err)
	}
	req.Header.Set("Accept", HeaderAccept)
	req.Header.Set("User-Agent", HeaderUserAgent)
	if token := strings.TrimSpace(c.getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(HeaderAuthorization, HeaderGithubTokenPref+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("request releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Response{}, fmt.Errorf("releases request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded []listResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Response{}, fmt.Errorf("decode releases response: %w", err)
	}

	for _, rel := range decoded {
		if rel.Draft || rel.Prerelease {
			continue
		}
		tag := strings.TrimSpace(rel.TagName)
		if tag == "" {
			continue
		}
		if strings.Contains(tag, "/") {
			continue
		}
		if !strings.HasPrefix(tag, "v") {
			continue
		}
		return Response{
			ID:      rel.ID,
			TagName: rel.TagName,
			Assets:  rel.Assets,
		}, nil
	}

	return Response{}, errors.New("no matching stable release found")
}

func (c *Client) FetchByTag(ctx context.Context, tag string) (Response, error) {
	normalizedTag := NormalizeVersionTag(tag)
	if normalizedTag == "" {
		return Response{}, errors.New("release tag is required")
	}

	requestURL := TagsURLPrefix + url.PathEscape(normalizedTag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Response{}, fmt.Errorf("create release tag request: %w", err)
	}
	req.Header.Set("Accept", HeaderAccept)
	req.Header.Set("User-Agent", HeaderUserAgent)
	if token := strings.TrimSpace(c.getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(HeaderAuthorization, HeaderGithubTokenPref+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("request release tag %s: %w", normalizedTag, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Response{}, &ReleaseNotFoundError{Tag: normalizedTag}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Response{}, fmt.Errorf("release tag request failed for %s: status=%d body=%s", normalizedTag, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded Response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Response{}, fmt.Errorf("decode release tag response for %s: %w", normalizedTag, err)
	}
	if strings.TrimSpace(decoded.TagName) == "" {
		return Response{}, fmt.Errorf("release tag response for %s is missing tag_name", normalizedTag)
	}

	return decoded, nil
}

func (c *Client) DownloadFile(ctx context.Context, url, destPath string, perm os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", HeaderUserAgent)
	if token := strings.TrimSpace(c.getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(HeaderAuthorization, HeaderGithubTokenPref+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("copy response to destination: %w", err)
	}
	return nil
}

func (c *Client) VerifyDownloadedAssets(ctx context.Context, downloaded map[string]string, detailf func(string, ...any)) error {
	if c.LookPath == nil {
		return errors.New("gh CLI is required for attestation verification but was not found in PATH")
	}
	if _, err := c.LookPath("gh"); err != nil {
		return errors.New("gh CLI is required for attestation verification but was not found in PATH")
	}
	if c.RunCommand == nil {
		return errors.New("gh attestation verification command runner is not configured")
	}

	names := make([]string, 0, len(downloaded))
	for name := range downloaded {
		names = append(names, name)
	}
	sort.Strings(names)

	certIdentity := "https://github.com/" + Repo + "/" + WorkflowPath + "@refs/heads/main"
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := downloaded[name]
		_, err := c.RunCommand(
			ctx,
			"gh",
			"attestation",
			"verify",
			path,
			"--repo",
			Repo,
			"--cert-identity",
			certIdentity,
			"--cert-oidc-issuer",
			AttestationOIDCIssuer,
		)
		if err != nil {
			return &AttestationVerificationError{Asset: name, Err: err}
		}
		if detailf != nil {
			detailf("verified attestation: %s", name)
		}
	}
	return nil
}
