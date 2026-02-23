package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchReleaseByTag_UsesTagEndpointAndNormalizesVersion(t *testing.T) {
	var requestedURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		if req.Header.Get("Accept") != httpsHeaderAccept {
			return nil, fmt.Errorf("missing accept header")
		}
		if req.Header.Get("User-Agent") != httpsHeaderUserAgent {
			return nil, fmt.Errorf("missing user-agent header")
		}

		body := `{"id":201,"tag_name":"v1.2.3","assets":[]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	m := &Manager{httpClient: client}
	release, err := m.fetchReleaseByTag(context.Background(), "1.2.3")
	if err != nil {
		t.Fatalf("fetchReleaseByTag returned error: %v", err)
	}

	if requestedURL != releaseTagsURLPrefix+"v1.2.3" {
		t.Fatalf("expected request to %q, got %q", releaseTagsURLPrefix+"v1.2.3", requestedURL)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("expected tag v1.2.3, got %q", release.TagName)
	}
}

func TestFetchReleaseByTag_NotFoundReturnsTypedError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`)), Header: make(http.Header)}, nil
	})}

	m := &Manager{httpClient: client}
	_, err := m.fetchReleaseByTag(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	if !errors.Is(err, errReleaseNotFound) {
		t.Fatalf("expected errors.Is(err, errReleaseNotFound), got %v", err)
	}

	var notFoundErr *releaseNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected releaseNotFoundError type, got %T", err)
	}
	if notFoundErr.Tag != "v9.9.9" {
		t.Fatalf("expected missing tag v9.9.9, got %q", notFoundErr.Tag)
	}
}
