package daemonclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_SaveGetListAndShutdown(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/daemon/v1/artifacts/save_text", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, map[string]any{"artifact": map[string]any{"ref": "r1", "name": "note/t1", "kind": "text", "mimeType": "text/plain", "sizeBytes": 5}})
	})
	h.HandleFunc("/daemon/v1/artifacts/get", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, map[string]any{
			"artifact":   map[string]any{"ref": "r1", "name": "note/t1", "kind": "text", "mimeType": "text/plain", "sizeBytes": 5},
			"dataBase64": base64.StdEncoding.EncodeToString([]byte("hello")),
		})
	})
	h.HandleFunc("/daemon/v1/artifacts/list", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, map[string]any{"items": []map[string]any{{"ref": "r1", "name": "note/t1", "kind": "text", "mimeType": "text/plain", "sizeBytes": 5}}})
	})
	h.HandleFunc("/daemon/v1/control/shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, map[string]any{"status": "shutting-down"})
	})

	srv := httptest.NewServer(h)
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	ctx := context.Background()

	a, err := c.SaveText(ctx, SaveTextRequest{Workspace: WorkspaceSelector{WorkspaceID: "global"}, Name: "note/t1", Text: "hello"})
	if err != nil {
		t.Fatalf("save text: %v", err)
	}
	if a.Ref != "r1" {
		t.Fatalf("unexpected save ref: %q", a.Ref)
	}

	got, err := c.Get(ctx, GetRequest{Workspace: WorkspaceSelector{WorkspaceID: "global"}, Selector: Selector{Name: "note/t1"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(got.DataBase64)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if string(payload) != "hello" {
		t.Fatalf("payload mismatch: %q", string(payload))
	}

	list, err := c.List(ctx, ListRequest{Workspace: WorkspaceSelector{WorkspaceID: "global"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "note/t1" {
		t.Fatalf("unexpected list output: %+v", list.Items)
	}

	shutdown, err := c.Shutdown(ctx)
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if shutdown.Status != "shutting-down" {
		t.Fatalf("shutdown status mismatch: %q", shutdown.Status)
	}
}

func TestClient_MapsRemoteError(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/daemon/v1/artifacts/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": map[string]any{"code": CodeUnauthorized, "message": "bad token"},
		}); err != nil {
			t.Fatalf("encode remote error: %v", err)
		}
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	_, err := c.List(context.Background(), ListRequest{Workspace: WorkspaceSelector{WorkspaceID: "global"}})
	if err == nil {
		t.Fatal("expected remote error")
	}
	re, ok := err.(*RemoteError)
	if !ok {
		t.Fatalf("expected RemoteError, got %T", err)
	}
	if re.Code != CodeUnauthorized {
		t.Fatalf("remote code mismatch: %q", re.Code)
	}
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data}); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
}
