package web

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsValidSubspaceSelector(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "global", in: globalSubspaceSelector, want: true},
		{name: "hash", in: validHash, want: true},
		{name: "uppercase global", in: "GLOBAL", want: true},
		{name: "uppercase hash", in: strings.ToUpper(validHash), want: true},
		{name: "invalid hash", in: "xyz", want: false},
		{name: "empty", in: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidSubspaceSelector(tc.in); got != tc.want {
				t.Fatalf("isValidSubspaceSelector(%q)=%v want=%v", tc.in, got, tc.want)
			}
		})
	}
}

func TestServiceFromQuerySubspaceSupportsGlobalAndHashExistence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-store-root")
	h := newWebHarnessAtRoot(t, root)

	if _, err := h.s.serviceFromQuerySubspace(globalSubspaceSelector); err != nil {
		t.Fatalf("expected global subspace to resolve, got error: %v", err)
	}
	if _, err := h.s.serviceFromQuerySubspace(""); err != nil {
		t.Fatalf("expected empty subspace selector to resolve to global, got error: %v", err)
	}

	hash := strings.Repeat("c", 64)
	if _, err := h.s.serviceFromQuerySubspace(hash); err == nil || !strings.Contains(err.Error(), "selected subspace not found") {
		t.Fatalf("expected missing hash subspace error, got: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, hash), 0o755); err != nil {
		t.Fatalf("mkdir hash subspace: %v", err)
	}
	if _, err := h.s.serviceFromQuerySubspace(hash); err != nil {
		t.Fatalf("expected existing hash subspace to resolve, got error: %v", err)
	}
}

func TestAPISubspacesIncludesGlobalAndSortedHashes(t *testing.T) {
	root := t.TempDir()
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	if err := os.MkdirAll(filepath.Join(root, hashB), 0o755); err != nil {
		t.Fatalf("mkdir hashB: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, hashA), 0o755); err != nil {
		t.Fatalf("mkdir hashA: %v", err)
	}

	h := newWebHarnessAtRoot(t, root)
	rr := h.request(http.MethodGet, "/api/subspaces", nil, nil)
	assertStatus(t, rr, http.StatusOK)

	res := decodeJSON[struct {
		Items []string `json:"items"`
	}](t, rr)

	want := []string{globalSubspaceSelector, hashA, hashB}
	if !reflect.DeepEqual(res.Items, want) {
		t.Fatalf("subspace items mismatch\nwant=%v\ngot=%v", want, res.Items)
	}
}

func TestAPISubspacesReturnsInternalServerErrorOnDiscoverFailure(t *testing.T) {
	parent := t.TempDir()
	filePath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h := newWebHarnessAtRoot(t, filePath)
	rr := h.request(http.MethodGet, "/api/subspaces", nil, nil)
	assertStatus(t, rr, http.StatusInternalServerError)
}
