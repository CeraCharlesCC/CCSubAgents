package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"local-artifact-mcp/internal/domain"
)

func TestIsValidSubspaceHash(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !isValidSubspaceHash(valid) {
		t.Fatalf("expected valid hash")
	}

	cases := []string{
		"",
		"abc",
		"0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef",
		"g123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde",
	}
	for _, tc := range cases {
		if isValidSubspaceHash(tc) {
			t.Fatalf("expected invalid hash: %q", tc)
		}
	}
}

func TestIsValidSubspaceSelector(t *testing.T) {
	validHash := strings.Repeat("a", 64)

	tests := []struct {
		name    string
		input   string
		expects bool
	}{
		{name: "global", input: globalSubspaceSelector, expects: true},
		{name: "hash", input: validHash, expects: true},
		{name: "uppercase global", input: "GLOBAL", expects: false},
		{name: "invalid hash", input: "xyz", expects: false},
		{name: "empty", input: "", expects: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidSubspaceSelector(tc.input); got != tc.expects {
				t.Fatalf("isValidSubspaceSelector(%q)=%v want=%v", tc.input, got, tc.expects)
			}
		})
	}
}

func TestDiscoverSubspacesIncludesGlobalAndSortedHashes(t *testing.T) {
	root := t.TempDir()
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)

	if err := os.MkdirAll(filepath.Join(root, hashB), 0o755); err != nil {
		t.Fatalf("mkdir hashB: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-subspace"), 0o755); err != nil {
		t.Fatalf("mkdir invalid: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, hashA), 0o755); err != nil {
		t.Fatalf("mkdir hashA: %v", err)
	}

	s := New(root)
	got, err := s.discoverSubspaces()
	if err != nil {
		t.Fatalf("discoverSubspaces error: %v", err)
	}

	want := []string{globalSubspaceSelector, hashA, hashB}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverSubspaces mismatch\nwant=%v\ngot=%v", want, got)
	}
}

func TestServiceFromQuerySubspaceSupportsGlobal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-store-root")
	s := New(root)

	if _, err := s.serviceFromQuerySubspace(globalSubspaceSelector); err != nil {
		t.Fatalf("expected global subspace to resolve, got error: %v", err)
	}

	hash := strings.Repeat("c", 64)
	if _, err := s.serviceFromQuerySubspace(hash); err == nil || !strings.Contains(err.Error(), "selected subspace not found") {
		t.Fatalf("expected missing hash subspace error, got: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, hash), 0o755); err != nil {
		t.Fatalf("mkdir hash subspace: %v", err)
	}
	if _, err := s.serviceFromQuerySubspace(hash); err != nil {
		t.Fatalf("expected existing hash subspace to resolve, got error: %v", err)
	}
}

func TestAPIArtifactsAndDeleteSupportGlobalSubspace(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	if _, err := s.serviceForSubspace(globalSubspaceSelector).SaveText(context.Background(), domain.SaveTextInput{Name: "global/item", Text: "ok"}); err != nil {
		t.Fatalf("seed global artifact: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/artifacts?subspace=global", nil)
	rr := httptest.NewRecorder()
	s.handleAPIArtifacts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}

	var listRes struct {
		Items []domain.Artifact `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listRes.Items) != 1 || listRes.Items[0].Name != "global/item" {
		t.Fatalf("unexpected list items: %+v", listRes.Items)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/artifacts?subspace=global&name=global/item", nil)
	deleteRR := httptest.NewRecorder()
	s.handleAPIArtifacts(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRR.Code, deleteRR.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/artifacts?subspace=global", nil)
	verifyRR := httptest.NewRecorder()
	s.handleAPIArtifacts(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify list status=%d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	var verifyRes struct {
		Items []domain.Artifact `json:"items"`
	}
	if err := json.Unmarshal(verifyRR.Body.Bytes(), &verifyRes); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if len(verifyRes.Items) != 0 {
		t.Fatalf("expected no remaining global artifacts, got: %+v", verifyRes.Items)
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

	s := New(root)
	req := httptest.NewRequest(http.MethodGet, "/api/subspaces", nil)
	rr := httptest.NewRecorder()
	s.handleAPISubspaces(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var res struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := []string{globalSubspaceSelector, hashA, hashB}
	if !reflect.DeepEqual(res.Items, want) {
		t.Fatalf("subspace items mismatch\nwant=%v\ngot=%v", want, res.Items)
	}
}
