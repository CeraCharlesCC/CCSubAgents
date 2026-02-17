package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

func TestHandleInsertTextSuccess(t *testing.T) {
	s := New(t.TempDir())
	csrfToken := mustIssueCSRFToken(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subspace", globalSubspaceSelector); err != nil {
		t.Fatalf("write subspace: %v", err)
	}
	if err := writer.WriteField("prefix", "plan/"); err != nil {
		t.Fatalf("write prefix: %v", err)
	}
	if err := writer.WriteField("limit", "200"); err != nil {
		t.Fatalf("write limit: %v", err)
	}
	if err := writer.WriteField(csrfFieldName, csrfToken); err != nil {
		t.Fatalf("write csrf token: %v", err)
	}
	if err := writer.WriteField("name", "plan/text"); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if err := writer.WriteField("text", "hello from text"); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/insert", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleInsert(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "msg=") {
		t.Fatalf("expected success redirect, got location=%q", rr.Header().Get("Location"))
	}

	svc := s.serviceForSubspace(globalSubspaceSelector)
	_, payload, err := svc.Get(context.Background(), domain.Selector{Name: "plan/text"})
	if err != nil {
		t.Fatalf("get inserted text artifact: %v", err)
	}
	if string(payload) != "hello from text" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}

func TestHandleInsertFileSuccess(t *testing.T) {
	s := New(t.TempDir())
	csrfToken := mustIssueCSRFToken(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subspace", globalSubspaceSelector); err != nil {
		t.Fatalf("write subspace: %v", err)
	}
	if err := writer.WriteField(csrfFieldName, csrfToken); err != nil {
		t.Fatalf("write csrf token: %v", err)
	}
	if err := writer.WriteField("name", "files/blob"); err != nil {
		t.Fatalf("write name: %v", err)
	}
	part, err := writer.CreateFormFile("file", "blob.bin")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := part.Write([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write file payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/insert", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleInsert(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	svc := s.serviceForSubspace(globalSubspaceSelector)
	art, payload, err := svc.Get(context.Background(), domain.Selector{Name: "files/blob"})
	if err != nil {
		t.Fatalf("get inserted blob artifact: %v", err)
	}
	if art.Kind != domain.ArtifactKindFile {
		t.Fatalf("expected file kind, got %s", art.Kind)
	}
	if !bytes.Equal(payload, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestHandleInsertRejectsMissingPayload(t *testing.T) {
	s := New(t.TempDir())
	csrfToken := mustIssueCSRFToken(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subspace", globalSubspaceSelector); err != nil {
		t.Fatalf("write subspace: %v", err)
	}
	if err := writer.WriteField(csrfFieldName, csrfToken); err != nil {
		t.Fatalf("write csrf token: %v", err)
	}
	if err := writer.WriteField("name", "plan/empty"); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/insert", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleInsert(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "choose+exactly+one+content+source") {
		t.Fatalf("unexpected redirect location: %q", loc)
	}
}

func TestHandleInsertRejectsMissingCSRFToken(t *testing.T) {
	s := New(t.TempDir())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subspace", globalSubspaceSelector); err != nil {
		t.Fatalf("write subspace: %v", err)
	}
	if err := writer.WriteField("name", "plan/text"); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if err := writer.WriteField("text", "hello"); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/insert", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleInsert(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "invalid+csrf+token") {
		t.Fatalf("expected invalid csrf token redirect, got location=%q", rr.Header().Get("Location"))
	}
}

func TestHandleDeleteSupportsMultipleNames(t *testing.T) {
	s := New(t.TempDir())
	svc := s.serviceForSubspace(globalSubspaceSelector)
	csrfToken := mustIssueCSRFToken(t)

	if _, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "bulk/a", Text: "1"}); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if _, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "bulk/b", Text: "2"}); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "bulk/")
	form.Set("limit", "200")
	form.Set(csrfFieldName, csrfToken)
	form.Add("name", "bulk/a")
	form.Add("name", "bulk/b")

	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleDelete(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "deleted+2+artifacts") {
		t.Fatalf("unexpected location: %q", rr.Header().Get("Location"))
	}

	items, err := svc.List(context.Background(), "bulk/", 10)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty list after delete, got %d items", len(items))
	}
}

func TestHandleDeleteRejectsMissingCSRFToken(t *testing.T) {
	s := New(t.TempDir())

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "bulk/")
	form.Set("limit", "200")
	form.Add("name", "bulk/a")

	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.handleDelete(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "invalid+csrf+token") {
		t.Fatalf("expected invalid csrf token redirect, got location=%q", rr.Header().Get("Location"))
	}
}

func TestAPIDeleteSupportsMultipleNames(t *testing.T) {
	s := New(t.TempDir())
	svc := s.serviceForSubspace(globalSubspaceSelector)

	if _, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "api/del-a", Text: "a"}); err != nil {
		t.Fatalf("seed del-a: %v", err)
	}
	if _, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "api/del-b", Text: "b"}); err != nil {
		t.Fatalf("seed del-b: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/artifacts?subspace=global&name=api/del-a&name=api/del-b", nil)
	rr := httptest.NewRecorder()
	s.handleAPIArtifacts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		DeletedCount int               `json:"deletedCount"`
		Artifacts    []domain.Artifact `json:"artifacts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.DeletedCount != 2 || len(res.Artifacts) != 2 {
		t.Fatalf("unexpected delete result: %+v", res)
	}
}

func TestAPISaveSupportsTextAndBlob(t *testing.T) {
	s := New(t.TempDir())

	textReq := httptest.NewRequest(http.MethodPost, "/api/artifacts?subspace=global", strings.NewReader(`{"name":"api/text","text":"hello"}`))
	textReq.Header.Set("Content-Type", "application/json")
	textRR := httptest.NewRecorder()
	s.handleAPIArtifacts(textRR, textReq)
	if textRR.Code != http.StatusCreated {
		t.Fatalf("text save status=%d body=%s", textRR.Code, textRR.Body.String())
	}

	blobData := base64.StdEncoding.EncodeToString([]byte{0x0a, 0x0b, 0x0c})
	blobReq := httptest.NewRequest(
		http.MethodPost,
		"/api/artifacts?subspace=global",
		strings.NewReader(`{"name":"api/blob","dataBase64":"`+blobData+`","mimeType":"application/octet-stream","filename":"blob.bin"}`),
	)
	blobReq.Header.Set("Content-Type", "application/json")
	blobRR := httptest.NewRecorder()
	s.handleAPIArtifacts(blobRR, blobReq)
	if blobRR.Code != http.StatusCreated {
		t.Fatalf("blob save status=%d body=%s", blobRR.Code, blobRR.Body.String())
	}

	svc := s.serviceForSubspace(globalSubspaceSelector)
	_, textPayload, err := svc.Get(context.Background(), domain.Selector{Name: "api/text"})
	if err != nil {
		t.Fatalf("get text artifact: %v", err)
	}
	if string(textPayload) != "hello" {
		t.Fatalf("unexpected text payload: %q", string(textPayload))
	}

	blobArtifact, blobPayload, err := svc.Get(context.Background(), domain.Selector{Name: "api/blob"})
	if err != nil {
		t.Fatalf("get blob artifact: %v", err)
	}
	if blobArtifact.Kind != domain.ArtifactKindFile {
		t.Fatalf("expected file kind, got %s", blobArtifact.Kind)
	}
	if !bytes.Equal(blobPayload, []byte{0x0a, 0x0b, 0x0c}) {
		t.Fatalf("unexpected blob payload: %v", blobPayload)
	}
}

func TestAPIDeleteRejectsMixedNameAndRef(t *testing.T) {
	s := New(t.TempDir())

	req := httptest.NewRequest(http.MethodDelete, "/api/artifacts?subspace=global&name=x&ref=y", nil)
	rr := httptest.NewRecorder()
	s.handleAPIArtifacts(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := res["error"].(string); got != "ref and name cannot both be provided" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestAPIDeletePrevalidatesAllSelectorsBeforeMutation(t *testing.T) {
	s := New(t.TempDir())
	svc := s.serviceForSubspace(globalSubspaceSelector)

	artifact, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "api/prevalidate", Text: "safe"})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/artifacts?subspace=global&ref="+url.QueryEscape(artifact.Ref)+"&ref=bad-ref", nil)
	rr := httptest.NewRecorder()
	s.handleAPIArtifacts(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid ref") {
		t.Fatalf("expected invalid ref error, got body=%s", rr.Body.String())
	}

	if _, _, err := svc.Get(context.Background(), domain.Selector{Name: "api/prevalidate"}); err != nil {
		t.Fatalf("expected seeded artifact to remain after prevalidation failure: %v", err)
	}
}

func TestHandleDeletePrevalidatesAllSelectorsBeforeMutation(t *testing.T) {
	s := New(t.TempDir())
	svc := s.serviceForSubspace(globalSubspaceSelector)
	csrfToken := mustIssueCSRFToken(t)

	artifact, err := svc.SaveText(context.Background(), domain.SaveTextInput{Name: "form/prevalidate", Text: "safe"})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "form/")
	form.Set("limit", "200")
	form.Set(csrfFieldName, csrfToken)
	form.Add("ref", artifact.Ref)
	form.Add("ref", "bad-ref")

	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleDelete(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "invalid+ref") {
		t.Fatalf("expected invalid ref redirect, got location=%q", rr.Header().Get("Location"))
	}

	if _, _, err := svc.Get(context.Background(), domain.Selector{Name: "form/prevalidate"}); err != nil {
		t.Fatalf("expected seeded artifact to remain after prevalidation failure: %v", err)
	}
}

func TestAPIDeleteSingleNotFoundKeepsLegacyPayload(t *testing.T) {
	s := New(t.TempDir())

	req := httptest.NewRequest(http.MethodDelete, "/api/artifacts?subspace=global&name=missing", nil)
	rr := httptest.NewRecorder()
	s.handleAPIArtifacts(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := res["error"].(string); got != "not found" {
		t.Fatalf("unexpected error: %q", got)
	}
	if _, ok := res["deletedCount"]; ok {
		t.Fatalf("unexpected deletedCount in legacy single-not-found payload: %+v", res)
	}
	if _, ok := res["artifacts"]; ok {
		t.Fatalf("unexpected artifacts in legacy single-not-found payload: %+v", res)
	}
}

func TestHandleDeleteSingleNotFoundKeepsLegacyRedirectError(t *testing.T) {
	s := New(t.TempDir())
	csrfToken := mustIssueCSRFToken(t)

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "")
	form.Set("limit", "200")
	form.Set(csrfFieldName, csrfToken)
	form.Add("name", "missing")

	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	rr := httptest.NewRecorder()
	s.handleDelete(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "err=not+found") {
		t.Fatalf("expected legacy not found redirect error, got location=%q", rr.Header().Get("Location"))
	}
}

func mustIssueCSRFToken(t *testing.T) string {
	t.Helper()
	token, err := issueCSRFToken()
	if err != nil {
		t.Fatalf("issue csrf token: %v", err)
	}
	return token
}
