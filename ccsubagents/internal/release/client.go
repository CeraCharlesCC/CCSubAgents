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

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/versiontag"
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
	maxErrorBodyBytes     = 4096
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

func (c *Client) applyCommonHeaders(req *http.Request, acceptJSON bool) {
	if req == nil {
		return
	}
	if acceptJSON {
		req.Header.Set("Accept", HeaderAccept)
	}
	req.Header.Set("User-Agent", HeaderUserAgent)
	if token := strings.TrimSpace(c.getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set(HeaderAuthorization, HeaderGithubTokenPref+token)
	}
}

func (c *Client) newRequest(ctx context.Context, method, requestURL string, acceptJSON bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	c.applyCommonHeaders(req, acceptJSON)
	return req, nil
}

func (c *Client) readErrorSnippet(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func (c *Client) FetchLatest(ctx context.Context) (Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, ReleasesURL, true)
	if err != nil {
		return Response{}, fmt.Errorf("create releases request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("request releases: %w", err)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("request releases failed: status=%d body=%s", resp.StatusCode, c.readErrorSnippet(resp))
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
		if tag[0] != 'v' && tag[0] != 'V' {
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
	normalizedTag := versiontag.Normalize(tag)
	if normalizedTag == "" {
		return Response{}, errors.New("release tag is required")
	}

	return c.fetchByExactTag(ctx, normalizedTag)
}

func (c *Client) FetchByExactTag(ctx context.Context, tag string) (Response, error) {
	trimmedTag := strings.TrimSpace(tag)
	if trimmedTag == "" {
		return Response{}, errors.New("release tag is required")
	}

	return c.fetchByExactTag(ctx, trimmedTag)
}

func (c *Client) fetchByExactTag(ctx context.Context, tag string) (Response, error) {
	requestURL := TagsURLPrefix + url.PathEscape(tag)
	req, err := c.newRequest(ctx, http.MethodGet, requestURL, true)
	if err != nil {
		return Response{}, fmt.Errorf("create release tag request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("request release tag %s: %w", tag, err)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode == http.StatusNotFound {
		return Response{}, &ReleaseNotFoundError{Tag: tag}
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("request release tag %s failed: status=%d body=%s", tag, resp.StatusCode, c.readErrorSnippet(resp))
	}

	var decoded Response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Response{}, fmt.Errorf("decode release tag response for %s: %w", tag, err)
	}
	if strings.TrimSpace(decoded.TagName) == "" {
		return Response{}, fmt.Errorf("release tag response for %s is missing tag_name", tag)
	}

	return decoded, nil
}

func (c *Client) DownloadFile(ctx context.Context, url, destPath string, perm os.FileMode) error {
	req, err := c.newRequest(ctx, http.MethodGet, url, false)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download request failed: status=%d body=%s", resp.StatusCode, c.readErrorSnippet(resp))
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer closeFile(f)

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("copy response to destination: %w", err)
	}
	return nil
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		_ = err
	}
}

func closeFile(f *os.File) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		_ = err
	}
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
