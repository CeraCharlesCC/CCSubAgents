package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
)

func TestServerContract_SaveResolveGetListDeleteRoundTrip(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	httpServer := httptest.NewServer(NewServer(engine, "test").Routes())
	defer httpServer.Close()

	client := NewHTTPClient(httpServer.URL, "")
	ctx := context.Background()
	workspace := WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID}

	saved, err := client.SaveText(ctx, SaveTextRequest{Workspace: workspace, Name: "plan/roundtrip", Text: "hello"})
	if err != nil {
		t.Fatalf("save text: %v", err)
	}

	resolved, err := client.Resolve(ctx, ResolveRequest{Workspace: workspace, Name: "plan/roundtrip"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Ref != saved.Ref {
		t.Fatalf("resolved ref mismatch: got=%s want=%s", resolved.Ref, saved.Ref)
	}

	got, err := client.Get(ctx, GetRequest{Workspace: workspace, Selector: Selector{Name: "plan/roundtrip"}})
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

	list, err := client.List(ctx, ListRequest{Workspace: workspace, Prefix: "plan/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "plan/roundtrip" {
		t.Fatalf("unexpected list output: %+v", list.Items)
	}

	if _, err := client.Delete(ctx, DeleteRequest{Workspace: workspace, Selector: Selector{Name: "plan/roundtrip"}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := client.Get(ctx, GetRequest{Workspace: workspace, Selector: Selector{Name: "plan/roundtrip"}}); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestServerContract_ExpectedPrevRefConflict(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	httpServer := httptest.NewServer(NewServer(engine, "test").Routes())
	defer httpServer.Close()

	client := NewHTTPClient(httpServer.URL, "")
	ctx := context.Background()
	workspace := WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID}

	first, err := client.SaveText(ctx, SaveTextRequest{Workspace: workspace, Name: "plan/cas", Text: "first"})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	_, err = client.SaveText(ctx, SaveTextRequest{
		Workspace:       workspace,
		Name:            "plan/cas",
		Text:            "second",
		ExpectedPrevRef: "20260227T000000Z-deadbeefdeadbeef",
	})
	if err == nil {
		t.Fatal("expected conflict from stale expectedPrevRef")
	}

	resolved, err := client.Resolve(ctx, ResolveRequest{Workspace: workspace, Name: "plan/cas"})
	if err != nil {
		t.Fatalf("resolve after conflict: %v", err)
	}
	if resolved.Ref != first.Ref {
		t.Fatalf("expected ref to remain %s, got %s", first.Ref, resolved.Ref)
	}
}

func TestServerContract_MethodNotAllowedUsesEnvelopeForArtifactsEndpoint(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	req := httptest.NewRequest(http.MethodGet, "/daemon/v1/artifacts/list", nil)
	rr := httptest.NewRecorder()
	NewServer(engine, "test").Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status mismatch: got=%d want=%d", rr.Code, http.StatusMethodNotAllowed)
	}
	if got := rr.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("allow header mismatch: got=%q want=%q", got, http.MethodPost)
	}

	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%q", err, rr.Body.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false, got %+v", env)
	}
	if env.Error == nil || env.Error.Code != CodeMethodNotAllowed {
		t.Fatalf("expected method-not-allowed envelope error, got %+v", env.Error)
	}
}

func TestServerContract_MethodNotAllowedUsesEnvelopeForHealthEndpoint(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	req := httptest.NewRequest(http.MethodPost, "/daemon/v1/health", nil)
	rr := httptest.NewRecorder()
	NewServer(engine, "test").Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status mismatch: got=%d want=%d", rr.Code, http.StatusMethodNotAllowed)
	}
	if got := rr.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("allow header mismatch: got=%q want=%q", got, http.MethodGet)
	}

	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%q", err, rr.Body.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false, got %+v", env)
	}
	if env.Error == nil || env.Error.Code != CodeMethodNotAllowed {
		t.Fatalf("expected method-not-allowed envelope error, got %+v", env.Error)
	}
}

func TestServerRejectsOversizedJSONBody_NotTruncationEOF(t *testing.T) {
	engine, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

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
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%q", err, rr.Body.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false, got %+v", env)
	}
	if env.Error == nil || env.Error.Code != CodeInvalidInput {
		t.Fatalf("expected invalid-input envelope error, got %+v", env.Error)
	}
}
