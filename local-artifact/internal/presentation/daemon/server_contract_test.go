package daemon

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerContract_SaveResolveGetListDeleteRoundTrip(t *testing.T) {
	h := newDaemonHTTPHarness(t)

	saved, err := h.client.SaveText(h.ctx, SaveTextRequest{Workspace: h.workspace, Name: "plan/roundtrip", Text: "hello"})
	if err != nil {
		t.Fatalf("save text: %v", err)
	}

	resolved, err := h.client.Resolve(h.ctx, ResolveRequest{Workspace: h.workspace, Name: "plan/roundtrip"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Ref != saved.Ref {
		t.Fatalf("resolved ref mismatch: got=%s want=%s", resolved.Ref, saved.Ref)
	}

	got, err := h.client.Get(h.ctx, GetRequest{Workspace: h.workspace, Selector: Selector{Name: "plan/roundtrip"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(got.DataBase64)
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if string(payload) != "hello" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}

	list, err := h.client.List(h.ctx, ListRequest{Workspace: h.workspace, Prefix: "plan/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "plan/roundtrip" {
		t.Fatalf("unexpected list output: %+v", list.Items)
	}

	if _, err := h.client.Delete(h.ctx, DeleteRequest{Workspace: h.workspace, Selector: Selector{Name: "plan/roundtrip"}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.client.Get(h.ctx, GetRequest{Workspace: h.workspace, Selector: Selector{Name: "plan/roundtrip"}}); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestServerContract_ExpectedPrevRefConflict(t *testing.T) {
	h := newDaemonHTTPHarness(t)

	first, err := h.client.SaveText(h.ctx, SaveTextRequest{Workspace: h.workspace, Name: "plan/cas", Text: "first"})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	_, err = h.client.SaveText(h.ctx, SaveTextRequest{
		Workspace:       h.workspace,
		Name:            "plan/cas",
		Text:            "second",
		ExpectedPrevRef: "20260227T000000Z-deadbeefdeadbeef",
	})
	if err == nil {
		t.Fatal("expected conflict from stale expectedPrevRef")
	}

	resolved, err := h.client.Resolve(h.ctx, ResolveRequest{Workspace: h.workspace, Name: "plan/cas"})
	if err != nil {
		t.Fatalf("resolve after conflict: %v", err)
	}
	if resolved.Ref != first.Ref {
		t.Fatalf("expected ref to remain %s, got %s", first.Ref, resolved.Ref)
	}
}

func TestServerContract_MethodNotAllowedUsesEnvelope(t *testing.T) {
	engine := newDaemonEngine(t)
	handler := NewServer(engine, "test").Routes()

	tests := []struct {
		name       string
		method     string
		path       string
		wantAllow  string
		wantStatus int
	}{
		{
			name:       "artifacts endpoint",
			method:     http.MethodGet,
			path:       "/daemon/v1/artifacts/list",
			wantAllow:  http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "health endpoint",
			method:     http.MethodPost,
			path:       "/daemon/v1/health",
			wantAllow:  http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status mismatch: got=%d want=%d", rr.Code, tc.wantStatus)
			}
			if got := rr.Header().Get("Allow"); got != tc.wantAllow {
				t.Fatalf("allow header mismatch: got=%q want=%q", got, tc.wantAllow)
			}

			env := decodeEnvelope(t, rr.Body.Bytes())
			if env.OK {
				t.Fatalf("expected ok=false, got %+v", env)
			}
			if env.Error == nil || env.Error.Code != CodeMethodNotAllowed {
				t.Fatalf("expected method-not-allowed envelope error, got %+v", env.Error)
			}
		})
	}
}

func TestServerRejectsOversizedJSONBody_NotTruncationEOF(t *testing.T) {
	engine := newDaemonEngine(t)

	srv := NewServer(engine, "test")
	srv.maxRequestBytes = 64

	body := `{"workspace":{"workspaceID":"global"},"name":"plan/oversized","text":"ok"}` + strings.Repeat(" ", 80)
	req := httptest.NewRequest(http.MethodPost, "/daemon/v1/artifacts/save_text", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status mismatch: got=%d want=%d body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	env := decodeEnvelope(t, rr.Body.Bytes())
	if env.OK {
		t.Fatalf("expected ok=false, got %+v", env)
	}
	if env.Error == nil || env.Error.Code != CodeInvalidInput {
		t.Fatalf("expected invalid-input envelope error, got %+v", env.Error)
	}
}
