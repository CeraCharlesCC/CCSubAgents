package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

func TestSanitizeFilenameRejectsPathTraversalMarkers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "dot", in: ".", want: ""},
		{name: "dotdot", in: "..", want: ""},
		{name: "windows-dotdot", in: `..\`, want: ""},
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

func TestParseDeleteSelectors(t *testing.T) {
	tests := []struct {
		name        string
		values      url.Values
		want        deleteSelectorRequest
		wantErr     error
		wantErrText string
	}{
		{
			name: "single name",
			values: url.Values{
				"name": {" plan/task-1 "},
			},
			want: deleteSelectorRequest{selectors: []domain.Selector{{Name: "plan/task-1"}}, single: true},
		},
		{
			name: "names dedupe and trim",
			values: url.Values{
				"name": {" a ", "a", "", "b ", "a"},
			},
			want: deleteSelectorRequest{selectors: []domain.Selector{{Name: "a"}, {Name: "b"}}, single: false},
		},
		{
			name: "refs dedupe and trim",
			values: url.Values{
				"ref": {" r1 ", "r1", "r2", "  "},
			},
			want: deleteSelectorRequest{selectors: []domain.Selector{{Ref: "r1"}, {Ref: "r2"}}, single: false},
		},
		{
			name: "mixed name and ref",
			values: url.Values{
				"name": {"x"},
				"ref":  {"y"},
			},
			wantErr: domain.ErrRefAndNameMutuallyExclusive,
		},
		{
			name:    "empty selectors",
			values:  url.Values{},
			wantErr: domain.ErrRefOrName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDeleteSelectors(tc.values)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseDeleteSelectors error=%v want=%v", err, tc.wantErr)
				}
				if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("parseDeleteSelectors error text=%q want contains %q", err.Error(), tc.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDeleteSelectors unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseDeleteSelectors mismatch\nwant=%+v\ngot=%+v", tc.want, got)
			}
		})
	}
}

func TestParseSingleSelector(t *testing.T) {
	tests := []struct {
		name        string
		values      url.Values
		want        domain.Selector
		wantErr     error
		wantErrText string
	}{
		{
			name: "single name",
			values: url.Values{
				"name": {" plan/item "},
			},
			want: domain.Selector{Name: "plan/item"},
		},
		{
			name: "single ref",
			values: url.Values{
				"ref": {" 20260216T120000Z-aaaaaaaaaaaaaaaa "},
			},
			want: domain.Selector{Ref: "20260216T120000Z-aaaaaaaaaaaaaaaa"},
		},
		{
			name:    "none provided",
			values:  url.Values{},
			wantErr: domain.ErrRefOrName,
		},
		{
			name: "multiple names",
			values: url.Values{
				"name": {"a", "b"},
			},
			wantErrText: "provide exactly one ref or name",
		},
		{
			name: "mixed name and ref",
			values: url.Values{
				"name": {"a"},
				"ref":  {"r"},
			},
			wantErr: domain.ErrRefAndNameMutuallyExclusive,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSingleSelector(tc.values)
			if tc.wantErr != nil || tc.wantErrText != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseSingleSelector error=%v want=%v", err, tc.wantErr)
				}
				if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("parseSingleSelector error text=%q want contains %q", err.Error(), tc.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSingleSelector unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseSingleSelector mismatch\nwant=%+v\ngot=%+v", tc.want, got)
			}
		})
	}
}

func TestTrimUniqueNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: []string{}},
		{name: "dedupe trim and preserve order", in: []string{" a ", "", "a", "b", " a", "b", " c "}, want: []string{"a", "b", "c"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimUniqueNonEmpty(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("trimUniqueNonEmpty mismatch\nwant=%v\ngot=%v", tc.want, got)
			}
		})
	}
}

func TestIsValidCSRFToken(t *testing.T) {
	validToken := mustCSRFToken(t)
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "valid", token: validToken, want: true},
		{name: "empty", token: "", want: false},
		{name: "short", token: "abc", want: false},
		{name: "non hex", token: strings.Repeat("z", csrfTokenBytes*2), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidCSRFToken(tc.token); got != tc.want {
				t.Fatalf("isValidCSRFToken(%q)=%v want=%v", tc.token, got, tc.want)
			}
		})
	}
}

func TestCSRFTokenFromRequest(t *testing.T) {
	validToken := mustCSRFToken(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "not-hex"})
	if got := csrfTokenFromRequest(req); got != "" {
		t.Fatalf("expected invalid cookie to be ignored, got %q", got)
	}

	reqValid := httptest.NewRequest(http.MethodGet, "/", nil)
	reqValid.AddCookie(&http.Cookie{Name: csrfCookieName, Value: validToken})
	if got := csrfTokenFromRequest(reqValid); got != validToken {
		t.Fatalf("expected valid cookie token %q, got %q", validToken, got)
	}
}

func TestValidateCSRFToken(t *testing.T) {
	validToken := mustCSRFToken(t)
	tests := []struct {
		name      string
		cookie    string
		formToken string
		wantErr   bool
	}{
		{name: "missing form token", cookie: validToken, formToken: "", wantErr: true},
		{name: "mismatch", cookie: validToken, formToken: mustCSRFToken(t), wantErr: true},
		{name: "invalid cookie", cookie: "invalid", formToken: validToken, wantErr: true},
		{name: "valid", cookie: validToken, formToken: validToken, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.formToken != "" {
				form.Set(csrfFieldName, tc.formToken)
			}
			req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tc.cookie})
			}

			err := validateCSRFToken(req)
			if tc.wantErr && err == nil {
				t.Fatal("expected csrf validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected csrf validation error: %v", err)
			}
		})
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

	_, _, gotLimit = formRedirectContext(url.Values{"limit": {" 17 "}})
	if gotLimit != "17" {
		t.Fatalf("unexpected explicit limit: %q", gotLimit)
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
