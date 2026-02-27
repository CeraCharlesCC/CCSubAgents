package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestAPISaveSupportsTextAndBlob(t *testing.T) {
	h := newWebHarness(t)

	textRR := h.jsonRequest(http.MethodPost, "/api/artifacts?subspace=global", `{"name":"api/text","text":"hello"}`)
	assertStatus(t, textRR, http.StatusCreated)

	blobData := base64.StdEncoding.EncodeToString([]byte{0x0a, 0x0b, 0x0c})
	blobRR := h.jsonRequest(
		http.MethodPost,
		"/api/artifacts?subspace=global",
		`{"name":"api/blob","dataBase64":"`+blobData+`","mimeType":"application/octet-stream","filename":"blob.bin"}`,
	)
	assertStatus(t, blobRR, http.StatusCreated)

	_, textPayload := h.mustGetByName(globalSubspaceSelector, "api/text")
	if string(textPayload) != "hello" {
		t.Fatalf("unexpected text payload: %q", string(textPayload))
	}

	blobArtifact, blobPayload := h.mustGetByName(globalSubspaceSelector, "api/blob")
	if blobArtifact.Kind != artifacts.ArtifactKindFile {
		t.Fatalf("expected file kind, got %q", blobArtifact.Kind)
	}
	if blobArtifact.Filename != "blob.bin" {
		t.Fatalf("expected filename blob.bin, got %q", blobArtifact.Filename)
	}
	if !bytes.Equal(blobPayload, []byte{0x0a, 0x0b, 0x0c}) {
		t.Fatalf("unexpected blob payload: %v", blobPayload)
	}
}

func TestAPISaveRejectsInvalidText(t *testing.T) {
	h := newWebHarness(t)

	rr := h.jsonRequest(http.MethodPost, "/api/artifacts?subspace=global", `{"name":"api/invalid","text":"  \n\t"}`)
	assertStatus(t, rr, http.StatusBadRequest)

	res := decodeJSON[map[string]any](t, rr)
	errText, _ := res["error"].(string)
	if !strings.Contains(errText, "text is required") {
		t.Fatalf("expected text-required error, got %q", errText)
	}
}

func TestAPIDeleteSupportsMultipleNames(t *testing.T) {
	h := newWebHarness(t)
	h.mustSaveText(globalSubspaceSelector, "api/del-a", "a")
	h.mustSaveText(globalSubspaceSelector, "api/del-b", "b")

	rr := h.request(http.MethodDelete, "/api/artifacts?subspace=global&name=api/del-a&name=api/del-b", nil, nil)
	assertStatus(t, rr, http.StatusOK)

	type deleteResponse struct {
		DeletedCount int                  `json:"deletedCount"`
		Artifacts    []artifacts.Artifact `json:"artifacts"`
	}
	res := decodeJSON[deleteResponse](t, rr)
	if res.DeletedCount != 2 || len(res.Artifacts) != 2 {
		t.Fatalf("unexpected delete result: %+v", res)
	}
}

func TestAPIDeleteRejectsMixedNameAndRef(t *testing.T) {
	h := newWebHarness(t)

	rr := h.request(http.MethodDelete, "/api/artifacts?subspace=global&name=x&ref=y", nil, nil)
	assertStatus(t, rr, http.StatusBadRequest)

	res := decodeJSON[map[string]any](t, rr)
	if got, _ := res["error"].(string); got != "ref and name cannot both be provided" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestAPIDeletePrevalidatesAllSelectorsBeforeMutation(t *testing.T) {
	h := newWebHarness(t)
	artifact := h.mustSaveText(globalSubspaceSelector, "api/prevalidate", "safe")

	rr := h.request(http.MethodDelete, "/api/artifacts?subspace=global&ref="+url.QueryEscape(artifact.Ref)+"&ref=bad-ref", nil, nil)
	assertStatus(t, rr, http.StatusBadRequest)

	res := decodeJSON[map[string]any](t, rr)
	errText, _ := res["error"].(string)
	if !strings.Contains(errText, "invalid ref") {
		t.Fatalf("expected invalid ref error, got %q", errText)
	}

	_, payload := h.mustGetByName(globalSubspaceSelector, "api/prevalidate")
	if string(payload) != "safe" {
		t.Fatalf("expected artifact payload to remain unchanged, got %q", string(payload))
	}
}

func TestAPIDeleteSingleNotFoundKeepsLegacyPayload(t *testing.T) {
	h := newWebHarness(t)

	rr := h.request(http.MethodDelete, "/api/artifacts?subspace=global&name=missing", nil, nil)
	assertStatus(t, rr, http.StatusNotFound)

	res := decodeJSON[map[string]any](t, rr)
	if got, _ := res["error"].(string); got != "not found" {
		t.Fatalf("unexpected error: %q", got)
	}
	if _, ok := res["deletedCount"]; ok {
		t.Fatalf("unexpected deletedCount in legacy payload: %+v", res)
	}
	if _, ok := res["artifacts"]; ok {
		t.Fatalf("unexpected artifacts in legacy payload: %+v", res)
	}
}

func TestAPIContentReturnsPayloadAndMetadataHeaders(t *testing.T) {
	h := newWebHarness(t)
	saved, err := h.svc(globalSubspaceSelector).SaveText(context.Background(), artifacts.SaveTextInput{
		Name:     "viewer/sample",
		Text:     "preview text",
		MimeType: "text/plain; charset=utf-8",
	})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	rr := h.request(http.MethodGet, "/api/artifact-content?subspace=global&ref="+url.QueryEscape(saved.Ref), nil, nil)
	assertStatus(t, rr, http.StatusOK)

	if got := rr.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if got := rr.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Fatalf("unexpected content disposition: %q", got)
	}
	if got := rr.Header().Get("X-Artifact-Name"); got != "viewer/sample" {
		t.Fatalf("unexpected artifact name header: %q", got)
	}
	if got := rr.Header().Get("X-Artifact-Ref"); got != saved.Ref {
		t.Fatalf("unexpected artifact ref header: %q", got)
	}
	if got := rr.Header().Get("X-Artifact-MimeType"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected artifact mime type header: %q", got)
	}
	if rr.Body.String() != "preview text" {
		t.Fatalf("unexpected payload: %q", rr.Body.String())
	}
}

func TestAPIContentRejectsMultipleSelectors(t *testing.T) {
	h := newWebHarness(t)

	rr := h.request(http.MethodGet, "/api/artifact-content?subspace=global&name=viewer/a&name=viewer/b", nil, nil)
	assertStatus(t, rr, http.StatusBadRequest)

	res := decodeJSON[map[string]any](t, rr)
	if got, _ := res["error"].(string); got != "provide exactly one ref or name" {
		t.Fatalf("unexpected error: %q", got)
	}
}
