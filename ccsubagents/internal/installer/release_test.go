package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
)

func TestFetchReleaseByTag_UsesTagEndpointAndNormalizesVersion(t *testing.T) {
	var requestedURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		if req.Header.Get("Accept") != release.HeaderAccept {
			return nil, fmt.Errorf("missing accept header")
		}
		if req.Header.Get("User-Agent") != release.HeaderUserAgent {
			return nil, fmt.Errorf("missing user-agent header")
		}

		body := `{"id":201,"tag_name":"v1.2.3","assets":[]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	m := &Runner{httpClient: client}
	rel, err := m.releaseClient().FetchByTag(context.Background(), "1.2.3")
	if err != nil {
		t.Fatalf("FetchByTag returned error: %v", err)
	}

	if requestedURL != release.TagsURLPrefix+"v1.2.3" {
		t.Fatalf("expected request to %q, got %q", release.TagsURLPrefix+"v1.2.3", requestedURL)
	}
	if rel.TagName != "v1.2.3" {
		t.Fatalf("expected tag v1.2.3, got %q", rel.TagName)
	}
}

func TestFetchReleaseByTag_NotFoundReturnsTypedError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`)), Header: make(http.Header)}, nil
	})}

	m := &Runner{httpClient: client}
	_, err := m.releaseClient().FetchByTag(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	if !errors.Is(err, release.ErrReleaseNotFound) {
		t.Fatalf("expected errors.Is(err, release.ErrReleaseNotFound), got %v", err)
	}

	var notFoundErr *release.ReleaseNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected release.ReleaseNotFoundError type, got %T", err)
	}
	if notFoundErr.Tag != "v9.9.9" {
		t.Fatalf("expected missing tag v9.9.9, got %q", notFoundErr.Tag)
	}
}

func TestFetchReleaseByTag_EscapesTagPathSegment(t *testing.T) {
	var requestedURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`)), Header: make(http.Header)}, nil
	})}

	m := &Runner{httpClient: client}
	_, err := m.releaseClient().FetchByTag(context.Background(), "v1/2.3")
	if err == nil {
		t.Fatalf("expected not-found error")
	}

	wantURL := release.TagsURLPrefix + "v1%2F2.3"
	if requestedURL != wantURL {
		t.Fatalf("expected request to %q, got %q", wantURL, requestedURL)
	}
}
