package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

func TestHandleInsertTextSuccess(t *testing.T) {
	h := newWebHarness(t)

	rr := h.postMultipart("/insert", map[string]string{
		"subspace": globalSubspaceSelector,
		"prefix":   "plan/",
		"limit":    "200",
		"name":     "plan/text",
		"text":     "hello from text",
	}, "", "", nil, true)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "msg=")

	_, payload := h.mustGetByName(globalSubspaceSelector, "plan/text")
	if string(payload) != "hello from text" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}

func TestHandleInsertFileSuccess(t *testing.T) {
	h := newWebHarness(t)

	rr := h.postMultipart("/insert", map[string]string{
		"subspace": globalSubspaceSelector,
		"name":     "files/blob",
	}, "file", "blob.bin", []byte{0x01, 0x02, 0x03}, true)

	assertStatus(t, rr, http.StatusSeeOther)

	art, payload := h.mustGetByName(globalSubspaceSelector, "files/blob")
	if art.Kind != domain.ArtifactKindFile {
		t.Fatalf("expected file kind, got %q", art.Kind)
	}
	if art.Filename != "blob.bin" {
		t.Fatalf("expected filename to be preserved, got %q", art.Filename)
	}
	if !bytes.Equal(payload, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestHandleInsertRejectsMissingCSRFToken(t *testing.T) {
	h := newWebHarness(t)

	rr := h.postMultipart("/insert", map[string]string{
		"subspace": globalSubspaceSelector,
		"name":     "plan/text",
		"text":     "hello",
	}, "", "", nil, false)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "invalid+csrf+token")
}

func TestHandleDeleteSupportsMultipleNames(t *testing.T) {
	h := newWebHarness(t)
	h.mustSaveText(globalSubspaceSelector, "bulk/a", "1")
	h.mustSaveText(globalSubspaceSelector, "bulk/b", "2")

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "bulk/")
	form.Set("limit", "200")
	form.Add("name", "bulk/a")
	form.Add("name", "bulk/b")

	rr := h.postForm("/delete", form, true)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "deleted+2+artifacts")

	items, err := h.svc(globalSubspaceSelector).List(context.Background(), "bulk/", 10)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty list after delete, got %d items", len(items))
	}
}

func TestHandleDeleteRejectsMissingCSRFToken(t *testing.T) {
	h := newWebHarness(t)

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "bulk/")
	form.Set("limit", "200")
	form.Add("name", "bulk/a")

	rr := h.postForm("/delete", form, false)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "invalid+csrf+token")
}

func TestHandleDeleteSingleNotFoundKeepsLegacyRedirectError(t *testing.T) {
	h := newWebHarness(t)

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "")
	form.Set("limit", "200")
	form.Add("name", "missing")

	rr := h.postForm("/delete", form, true)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "err=not+found")
}

func TestHandleDeletePrevalidatesAllSelectorsBeforeMutation(t *testing.T) {
	h := newWebHarness(t)
	artifact := h.mustSaveText(globalSubspaceSelector, "form/prevalidate", "safe")

	form := url.Values{}
	form.Set("subspace", globalSubspaceSelector)
	form.Set("prefix", "form/")
	form.Set("limit", "200")
	form.Add("ref", artifact.Ref)
	form.Add("ref", "bad-ref")

	rr := h.postForm("/delete", form, true)

	assertStatus(t, rr, http.StatusSeeOther)
	assertRedirectContains(t, rr, "invalid+ref")

	_, payload := h.mustGetByName(globalSubspaceSelector, "form/prevalidate")
	if string(payload) != "safe" {
		t.Fatalf("expected artifact payload to remain unchanged, got %q", string(payload))
	}
}

func TestHandleIndexReturnsInternalServerErrorWhenCSRFTokenIssueFails(t *testing.T) {
	h := newWebHarness(t)
	originalReadRandom := csrfReadRandom
	csrfReadRandom = func(_ []byte) (int, error) {
		return 0, errors.New("entropy unavailable")
	}
	t.Cleanup(func() {
		csrfReadRandom = originalReadRandom
	})

	rr := h.request(http.MethodGet, "/", nil, nil)

	assertStatus(t, rr, http.StatusInternalServerError)
	if got := rr.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("expected no csrf cookie when issuance fails, got %q", got)
	}
}
