package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

type webHarness struct {
	t *testing.T
	s *Server
}

func newWebHarness(t *testing.T) *webHarness {
	t.Helper()
	return &webHarness{
		t: t,
		s: New(t.TempDir()),
	}
}

func (h *webHarness) svc(subspace string) *artifacts.Service {
	h.t.Helper()
	svc, err := h.s.serviceForSubspace(subspace)
	if err != nil {
		h.t.Fatalf("serviceForSubspace(%q): %v", subspace, err)
	}
	return svc
}

func (h *webHarness) mustSaveText(subspace, name, text string) artifacts.Artifact {
	h.t.Helper()
	art, err := h.svc(subspace).SaveText(context.Background(), artifacts.SaveTextInput{Name: name, Text: text})
	if err != nil {
		h.t.Fatalf("save text %q: %v", name, err)
	}
	return art
}

func (h *webHarness) mustGetByName(subspace, name string) (artifacts.Artifact, []byte) {
	h.t.Helper()
	art, payload, err := h.svc(subspace).Get(context.Background(), artifacts.Selector{Name: name})
	if err != nil {
		h.t.Fatalf("get %q: %v", name, err)
	}
	return art, payload
}

func (h *webHarness) request(method, path string, body io.Reader, mutate func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if mutate != nil {
		mutate(req)
	}
	rr := httptest.NewRecorder()
	h.s.routes().ServeHTTP(rr, req)
	return rr
}

func (h *webHarness) jsonRequest(method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.request(method, path, strings.NewReader(body), func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
	})
}

func (h *webHarness) postForm(path string, values url.Values, withCSRF bool) *httptest.ResponseRecorder {
	h.t.Helper()
	form := cloneValues(values)
	csrfToken := ""
	if withCSRF {
		csrfToken = mustCSRFToken(h.t)
		form.Set(csrfFieldName, csrfToken)
	}
	return h.request(http.MethodPost, path, strings.NewReader(form.Encode()), func(req *http.Request) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if withCSRF {
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
		}
	})
}

func (h *webHarness) postMultipart(path string, fields map[string]string, fileField string, filename string, data []byte, withCSRF bool) *httptest.ResponseRecorder {
	h.t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, key := range sortedKeys(fields) {
		if err := writer.WriteField(key, fields[key]); err != nil {
			h.t.Fatalf("write field %q: %v", key, err)
		}
	}
	csrfToken := ""
	if withCSRF {
		csrfToken = mustCSRFToken(h.t)
		if err := writer.WriteField(csrfFieldName, csrfToken); err != nil {
			h.t.Fatalf("write csrf token: %v", err)
		}
	}
	if strings.TrimSpace(fileField) != "" {
		part, err := writer.CreateFormFile(fileField, filename)
		if err != nil {
			h.t.Fatalf("create file field %q: %v", fileField, err)
		}
		if _, err := part.Write(data); err != nil {
			h.t.Fatalf("write file data: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		h.t.Fatalf("close multipart writer: %v", err)
	}

	return h.request(http.MethodPost, path, body, func(req *http.Request) {
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if withCSRF {
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
		}
	})
}

func mustCSRFToken(t *testing.T) string {
	t.Helper()
	token, err := issueCSRFToken()
	if err != nil {
		t.Fatalf("issue csrf token: %v", err)
	}
	return token
}

func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, want, rr.Body.String())
	}
}

func assertRedirectContains(t *testing.T, rr *httptest.ResponseRecorder, expected string) {
	t.Helper()
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, expected) {
		t.Fatalf("expected location to contain %q, got %q", expected, loc)
	}
}

func decodeJSON[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode json response: %v\nbody=%s", err, rr.Body.String())
	}
	return out
}

func cloneValues(values url.Values) url.Values {
	out := make(url.Values, len(values))
	for key, vals := range values {
		out[key] = append([]string(nil), vals...)
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
