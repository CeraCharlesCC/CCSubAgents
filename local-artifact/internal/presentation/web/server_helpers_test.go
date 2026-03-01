package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestSanitizeFilenameRejectsPathTraversalMarkers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "dotdot", in: "..", want: ""},
		{name: "windows-dotdot", in: `..\\`, want: ""},
		{name: "normalized basename", in: "../../safe.bin", want: "safe.bin"},
		{name: "control chars stripped", in: "a\x00b.bin", want: "ab.bin"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeFilename(tc.in); got != tc.want {
				t.Fatalf("sanitizeFilename(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseDeleteSelectors_EdgeCases(t *testing.T) {
	got, err := parseDeleteSelectors(url.Values{"name": {" a ", "a", "", "b "}})
	if err != nil {
		t.Fatalf("parseDeleteSelectors unexpected error: %v", err)
	}
	want := []artifacts.Selector{{Name: "a"}, {Name: "b"}}
	if len(got.selectors) != len(want) || got.selectors[0] != want[0] || got.selectors[1] != want[1] {
		t.Fatalf("parseDeleteSelectors mismatch\nwant=%+v\ngot=%+v", want, got.selectors)
	}
	if got.single {
		t.Fatalf("expected single=false for multi-delete input")
	}

	_, err = parseDeleteSelectors(url.Values{"name": {"x"}, "ref": {"y"}})
	if !errors.Is(err, artifacts.ErrRefAndNameMutuallyExclusive) {
		t.Fatalf("expected ref-and-name mutual exclusion error, got %v", err)
	}
}

func TestParseSingleSelector_EdgeCases(t *testing.T) {
	got, err := parseSingleSelector(url.Values{"name": {" plan/item "}})
	if err != nil {
		t.Fatalf("parseSingleSelector unexpected error: %v", err)
	}
	if got.Name != "plan/item" || got.Ref != "" {
		t.Fatalf("unexpected selector: %+v", got)
	}

	_, err = parseSingleSelector(url.Values{"name": {"a", "b"}})
	if err == nil || !strings.Contains(err.Error(), "provide exactly one ref or name") {
		t.Fatalf("expected single-selector count error, got %v", err)
	}
}

func TestValidateCSRFToken_MismatchRejected(t *testing.T) {
	cookieToken := mustCSRFToken(t)
	formToken := mustCSRFToken(t)
	if cookieToken == formToken {
		t.Fatal("expected distinct CSRF tokens for mismatch test")
	}

	form := url.Values{csrfFieldName: {formToken}}
	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: cookieToken})
	if err := validateCSRFToken(req); err == nil {
		t.Fatal("expected csrf validation mismatch error")
	}

	validReq := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(url.Values{csrfFieldName: {cookieToken}}.Encode()))
	validReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validReq.AddCookie(&http.Cookie{Name: csrfCookieName, Value: cookieToken})
	if err := validateCSRFToken(validReq); err != nil {
		t.Fatalf("expected valid csrf token pair, got %v", err)
	}
}

func TestAPISaveJSONBodyLimitAllowsMaxBinaryUpload(t *testing.T) {
	encodedPayloadBytes := int64(base64.StdEncoding.EncodedLen(int(maxInsertUploadBytes)))
	requiredLimit := encodedPayloadBytes + maxInsertUploadOverheadBytes
	if maxInsertJSONBodyBytes < requiredLimit {
		t.Fatalf(
			"json body limit too small: got %d, need at least %d to carry 10 MiB base64 payload",
			maxInsertJSONBodyBytes,
			requiredLimit,
		)
	}
}

func TestIsValidCSRFToken_Basic(t *testing.T) {
	validToken := mustCSRFToken(t)
	if !isValidCSRFToken(validToken) {
		t.Fatalf("expected valid token %q", validToken)
	}
	if isValidCSRFToken("") {
		t.Fatal("expected empty token to be invalid")
	}
	if isValidCSRFToken(strings.Repeat("z", csrfTokenBytes*2)) {
		t.Fatal("expected non-hex token to be invalid")
	}
}

func TestCSRFTokenFromRequest_IgnoresInvalidAndReturnsValid(t *testing.T) {
	validToken := mustCSRFToken(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "not-hex"})
	if got := csrfTokenFromRequest(req); got != "" {
		t.Fatalf("expected invalid cookie token to be ignored, got %q", got)
	}

	reqValid := httptest.NewRequest(http.MethodGet, "/", nil)
	reqValid.AddCookie(&http.Cookie{Name: csrfCookieName, Value: validToken})
	if got := csrfTokenFromRequest(reqValid); got != validToken {
		t.Fatalf("expected valid cookie token %q, got %q", validToken, got)
	}
}

func TestFormRedirectContextDefaultsAndTrim(t *testing.T) {
	gotSubspace, gotPrefix, gotLimit := formRedirectContext(url.Values{
		"subspace": {" GLOBAL "},
		"prefix":   {"  plan/next "},
		"limit":    {"   "},
	})

	if gotSubspace != "GLOBAL" {
		t.Fatalf("unexpected subspace: %q", gotSubspace)
	}
	if gotPrefix != "plan/next" {
		t.Fatalf("unexpected prefix: %q", gotPrefix)
	}
	if gotLimit != "200" {
		t.Fatalf("unexpected default limit: %q", gotLimit)
	}
}

func TestIndexRedirectBaseDefaultsLimitAndEscapes(t *testing.T) {
	base := indexRedirectBase("global", "a/b c", "")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse redirect base: %v", err)
	}
	q := u.Query()
	if q.Get("subspace") != "global" {
		t.Fatalf("unexpected subspace query: %q", q.Get("subspace"))
	}
	if q.Get("prefix") != "a/b c" {
		t.Fatalf("unexpected prefix query: %q", q.Get("prefix"))
	}
	if q.Get("limit") != "200" {
		t.Fatalf("unexpected limit query: %q", q.Get("limit"))
	}
}
