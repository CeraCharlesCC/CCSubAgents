package release

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchLatest_SetsJSONHeadersAndAuth(t *testing.T) {
	client := &Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Accept") != HeaderAccept {
				t.Fatalf("expected Accept=%q, got %q", HeaderAccept, req.Header.Get("Accept"))
			}
			if req.Header.Get("User-Agent") != HeaderUserAgent {
				t.Fatalf("expected User-Agent=%q, got %q", HeaderUserAgent, req.Header.Get("User-Agent"))
			}
			if req.Header.Get(HeaderAuthorization) != HeaderGithubTokenPref+"token123" {
				t.Fatalf("expected Authorization header to use github token")
			}
			body := `[{"id":1,"tag_name":"v1.2.3","draft":false,"prerelease":false,"assets":[]}]`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})},
		Getenv: func(key string) string {
			if key == "GITHUB_TOKEN" {
				return "token123"
			}
			return ""
		},
	}

	rel, err := client.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest returned error: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Fatalf("expected tag v1.2.3, got %q", rel.TagName)
	}
}

func TestFetchByTag_NotFoundReturnsTypedError(t *testing.T) {
	var requestedURL string
	client := &Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	_, err := client.FetchByTag(context.Background(), "1/2.3")
	if err == nil {
		t.Fatalf("expected not-found error")
	}

	wantURL := TagsURLPrefix + "v1%2F2.3"
	if requestedURL != wantURL {
		t.Fatalf("expected request to %q, got %q", wantURL, requestedURL)
	}
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("expected errors.Is(err, ErrReleaseNotFound), got %v", err)
	}

	var notFoundErr *ReleaseNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected ReleaseNotFoundError, got %T", err)
	}
	if notFoundErr.Tag != "v1/2.3" {
		t.Fatalf("expected missing tag v1/2.3, got %q", notFoundErr.Tag)
	}
}

func TestDownloadFile_SetsHeadersAndWritesFile(t *testing.T) {
	client := &Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Accept"); got != "" {
				t.Fatalf("expected download request to omit Accept header, got %q", got)
			}
			if req.Header.Get("User-Agent") != HeaderUserAgent {
				t.Fatalf("expected User-Agent=%q, got %q", HeaderUserAgent, req.Header.Get("User-Agent"))
			}
			if req.Header.Get(HeaderAuthorization) != HeaderGithubTokenPref+"token123" {
				t.Fatalf("expected Authorization header to use github token")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("payload")),
				Header:     make(http.Header),
			}, nil
		})},
		Getenv: func(key string) string {
			if key == "GITHUB_TOKEN" {
				return "token123"
			}
			return ""
		},
	}

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	if err := client.DownloadFile(context.Background(), "https://example.invalid/file", dest, 0o644); err != nil {
		t.Fatalf("DownloadFile returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("expected payload written to destination, got %q", string(got))
	}
}

func TestFetchLatest_ErrorBodyIsTrimmedAndBounded(t *testing.T) {
	longBody := " \n" + strings.Repeat("x", 5000) + "\t "
	client := &Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(longBody)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	_, err := client.FetchLatest(context.Background())
	if err == nil {
		t.Fatalf("expected FetchLatest to fail")
	}

	msg := err.Error()
	if !strings.Contains(msg, "status=500") {
		t.Fatalf("expected status code in error, got %q", msg)
	}

	idx := strings.Index(msg, "body=")
	if idx == -1 {
		t.Fatalf("expected body snippet in error, got %q", msg)
	}
	snippet := msg[idx+len("body="):]
	if len(snippet) == 0 {
		t.Fatalf("expected non-empty body snippet")
	}
	if len(snippet) > maxErrorBodyBytes {
		t.Fatalf("expected snippet length <= %d, got %d", maxErrorBodyBytes, len(snippet))
	}
	if strings.TrimSpace(snippet) != snippet {
		t.Fatalf("expected trimmed snippet, got %q", snippet)
	}
}
