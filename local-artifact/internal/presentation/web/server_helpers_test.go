package web

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

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
	raw, err := hex.DecodeString(cookieToken)
	if err != nil || len(raw) == 0 {
		t.Fatalf("failed to decode csrf token %q: %v", cookieToken, err)
	}
	raw[0] ^= 0x01
	formToken := hex.EncodeToString(raw)

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

func TestValidateMutationOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		host    string
		wantErr bool
	}{
		{name: "missing origin allowed", origin: "", host: "127.0.0.1:19130", wantErr: false},
		{name: "same localhost origin allowed", origin: "http://localhost:19130", host: "localhost:19130", wantErr: false},
		{name: "same ipv4 loopback origin allowed", origin: "http://127.0.0.1:19130", host: "127.0.0.1:19130", wantErr: false},
		{name: "same ipv6 loopback origin allowed", origin: "http://[::1]:19130", host: "[::1]:19130", wantErr: false},
		{name: "rebound host blocked", origin: "http://attacker.invalid:19130", host: "attacker.invalid:19130", wantErr: true},
		{name: "cross origin blocked", origin: "http://attacker.invalid:19130", host: "127.0.0.1:19130", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/artifacts", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			req.Host = tc.host

			err := validateMutationOrigin(req)
			if tc.wantErr && err == nil {
				t.Fatal("expected origin validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected origin validation error: %v", err)
			}
		})
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
	gotSubspace, gotPrefix, gotSort, gotLimit := formRedirectContext(url.Values{
		"subspace": {" GLOBAL "},
		"prefix":   {"  plan/next, report/ "},
		"sort":     {" time_desc "},
		"limit":    {"   "},
	})

	if gotSubspace != "GLOBAL" {
		t.Fatalf("unexpected subspace: %q", gotSubspace)
	}
	if gotPrefix != "plan/next, report/" {
		t.Fatalf("unexpected prefix: %q", gotPrefix)
	}
	if gotSort != listSortTimeDesc {
		t.Fatalf("unexpected sort: %q", gotSort)
	}
	if gotLimit != "200" {
		t.Fatalf("unexpected default limit: %q", gotLimit)
	}
}

func TestIndexRedirectBaseDefaultsLimitAndEscapes(t *testing.T) {
	base := indexRedirectBase("global", "a/b c,report/", listSortTimeAsc, "")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse redirect base: %v", err)
	}
	q := u.Query()
	if q.Get("subspace") != "global" {
		t.Fatalf("unexpected subspace query: %q", q.Get("subspace"))
	}
	if q.Get("prefix") != "a/b c,report/" {
		t.Fatalf("unexpected prefix query: %q", q.Get("prefix"))
	}
	if q.Get("sort") != listSortTimeAsc {
		t.Fatalf("unexpected sort query: %q", q.Get("sort"))
	}
	if q.Get("limit") != "200" {
		t.Fatalf("unexpected limit query: %q", q.Get("limit"))
	}
}

func TestSplitPrefixFilters_DedupesAndTrims(t *testing.T) {
	got := splitPrefixFilters(" plan/, report/ , plan/ , ,image/ ")
	want := []string{"plan/", "report/", "image/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitPrefixFilters mismatch\nwant=%v\ngot=%v", want, got)
	}
}

func TestSortArtifactVersions_ByTimeAndName(t *testing.T) {
	base := time.Date(2026, time.February, 20, 10, 0, 0, 0, time.UTC)
	items := []artifacts.ArtifactVersion{
		{Name: "zeta", CreatedAt: base},
		{Name: "alpha", CreatedAt: base.Add(2 * time.Minute)},
		{Name: "beta", CreatedAt: base.Add(2 * time.Minute)},
	}

	sortArtifactVersions(items, listSortTimeDesc)
	if items[0].Name != "alpha" || items[1].Name != "beta" || items[2].Name != "zeta" {
		t.Fatalf("unexpected time_desc order: %+v", []string{items[0].Name, items[1].Name, items[2].Name})
	}

	sortArtifactVersions(items, listSortTimeAsc)
	if items[0].Name != "zeta" || items[1].Name != "alpha" || items[2].Name != "beta" {
		t.Fatalf("unexpected time_asc order: %+v", []string{items[0].Name, items[1].Name, items[2].Name})
	}
}
