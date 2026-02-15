package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type indexNames struct {
	Names map[string]string `json:"names"`
}

type protocolMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type protocolHarness struct {
	t *testing.T

	inW  *io.PipeWriter
	outR *io.PipeReader

	outCh     chan protocolMessage
	readErrCh chan error
	serveErr  chan error

	cancel context.CancelFunc
}

func newProtocolHarness(t *testing.T, baseStoreRoot string) *protocolHarness {
	t.Helper()

	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	h := &protocolHarness{
		t:         t,
		inW:       serverInW,
		outR:      serverOutR,
		outCh:     make(chan protocolMessage, 16),
		readErrCh: make(chan error, 1),
		serveErr:  make(chan error, 1),
		cancel:    cancel,
	}

	s := New(baseStoreRoot)
	go func() {
		h.serveErr <- s.Serve(ctx, serverInR, serverOutW)
	}()
	go h.readLoop()

	t.Cleanup(h.close)
	return h
}

func (h *protocolHarness) close() {
	h.cancel()
	_ = h.inW.Close()
	_ = h.outR.Close()
	select {
	case <-h.serveErr:
	case <-time.After(2 * time.Second):
	}
}

func (h *protocolHarness) readLoop() {
	scanner := bufio.NewScanner(h.outR)
	for scanner.Scan() {
		var msg protocolMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			h.readErrCh <- err
			return
		}
		h.outCh <- msg
	}
	if err := scanner.Err(); err != nil {
		h.readErrCh <- err
	}
}

func (h *protocolHarness) send(v any) {
	h.t.Helper()
	if err := json.NewEncoder(h.inW).Encode(v); err != nil {
		h.t.Fatalf("send message: %v", err)
	}
}

func (h *protocolHarness) recv() protocolMessage {
	h.t.Helper()
	select {
	case err := <-h.readErrCh:
		h.t.Fatalf("read server output: %v", err)
	case msg := <-h.outCh:
		return msg
	case <-time.After(2 * time.Second):
		h.t.Fatal("timed out waiting for server output")
	}
	return protocolMessage{}
}

func readIndexNames(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index %s: %v", path, err)
	}
	var idx indexNames
	if err := json.Unmarshal(b, &idx); err != nil {
		t.Fatalf("unmarshal index %s: %v", path, err)
	}
	if idx.Names == nil {
		return map[string]string{}
	}
	return idx.Names
}

func TestNormalizeRootURIs_SortsAndDeduplicates(t *testing.T) {
	in := []string{
		" file:///repo/b/../b ",
		"file://localhost/repo/a",
		"FILE:///repo/a",
		"https://example.com/not-allowed",
	}

	got, err := normalizeRootURIs(in)
	if err != nil {
		t.Fatalf("normalizeRootURIs returned error: %v", err)
	}

	want := []string{"file:///repo/a", "file:///repo/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeRootURIs mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestComputeSubspaceHash_OrderInsensitiveAfterNormalization(t *testing.T) {
	first, err := normalizeRootURIs([]string{"file:///repo/b", "file:///repo/a"})
	if err != nil {
		t.Fatalf("normalize first: %v", err)
	}
	second, err := normalizeRootURIs([]string{"file:///repo/a", "file:///repo/b"})
	if err != nil {
		t.Fatalf("normalize second: %v", err)
	}

	h1 := computeSubspaceHash(first)
	h2 := computeSubspaceHash(second)
	if h1 != h2 {
		t.Fatalf("expected equal hashes, got %s and %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hash, got len=%d", len(h1))
	}
}

func TestRootsListProtocol_SuccessUsesScopedStore(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": true},
			},
		},
	})
	initializeResp := h.recv()
	if string(initializeResp.ID) != "1" || initializeResp.Error != nil {
		t.Fatalf("unexpected initialize response: %+v", initializeResp)
	}

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	rootsReq := h.recv()
	if rootsReq.Method != "roots/list" {
		t.Fatalf("expected roots/list request, got: %+v", rootsReq)
	}
	if len(rootsReq.ID) == 0 {
		t.Fatalf("expected roots/list request id, got: %+v", rootsReq)
	}

	roots := []map[string]any{{"uri": "file:///repo/a"}, {"uri": "file://localhost/repo/b/../b"}}
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(rootsReq.ID),
		"result": map[string]any{
			"roots": roots,
		},
	})

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolArtifactSaveText,
			"arguments": map[string]any{"name": "tests/protocol-success", "text": "ok"},
		},
	})
	toolResp := h.recv()
	if string(toolResp.ID) != "2" || toolResp.Error != nil {
		t.Fatalf("unexpected tool response: %+v", toolResp)
	}

	normalized, err := normalizeRootURIs([]string{"file:///repo/a", "file://localhost/repo/b/../b"})
	if err != nil {
		t.Fatalf("normalize roots: %v", err)
	}
	hash := computeSubspaceHash(normalized)

	if _, err := os.Stat(filepath.Join(storeRoot, hash, "names.json")); err != nil {
		t.Fatalf("expected scoped store index to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "names.json")); !os.IsNotExist(err) {
		t.Fatalf("expected global store index to be absent, got err=%v", err)
	}
}

func TestRootsListProtocol_DefersRootsListUntilInitializedNotification(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": true},
			},
		},
	})
	initializeResp := h.recv()
	if string(initializeResp.ID) != "1" || initializeResp.Error != nil {
		t.Fatalf("unexpected initialize response: %+v", initializeResp)
	}

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolArtifactSaveText,
			"arguments": map[string]any{"name": "tests/pre-init", "text": "ok"},
		},
	})
	preInitResp := h.recv()
	if preInitResp.Method == "roots/list" {
		t.Fatalf("unexpected roots/list before initialized notification: %+v", preInitResp)
	}
	if string(preInitResp.ID) != "2" || preInitResp.Error != nil {
		t.Fatalf("unexpected pre-init tool response: %+v", preInitResp)
	}

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	rootsReq := h.recv()
	if rootsReq.Method != "roots/list" {
		t.Fatalf("expected roots/list request after initialized notification, got: %+v", rootsReq)
	}
}

func TestRootsListProtocol_FallbackOnMethodNotFoundAndInternalError(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{name: "method not found", code: -32601},
		{name: "internal error", code: -32603},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storeRoot := t.TempDir()
			h := newProtocolHarness(t, storeRoot)

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"capabilities": map[string]any{
						"roots": map[string]any{"listChanged": true},
					},
				},
			})
			_ = h.recv()

			h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
			rootsReq := h.recv()
			if rootsReq.Method != "roots/list" {
				t.Fatalf("expected roots/list request, got: %+v", rootsReq)
			}

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rootsReq.ID),
				"error": map[string]any{
					"code":    tc.code,
					"message": "simulated",
				},
			})

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      toolArtifactSaveText,
					"arguments": map[string]any{"name": "tests/protocol-fallback", "text": "ok"},
				},
			})
			toolResp := h.recv()
			if string(toolResp.ID) != "2" || toolResp.Error != nil {
				t.Fatalf("unexpected tool response: %+v", toolResp)
			}

			if _, err := os.Stat(filepath.Join(storeRoot, "names.json")); err != nil {
				t.Fatalf("expected global fallback store index to exist: %v", err)
			}
		})
	}
}

func TestRootsListChangedProtocol_ReResolvesToNewScopedStore(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": true},
			},
		},
	})
	_ = h.recv()

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	firstRootsReq := h.recv()
	if firstRootsReq.Method != "roots/list" {
		t.Fatalf("expected first roots/list request, got: %+v", firstRootsReq)
	}

	firstRoots := []map[string]any{{"uri": "file:///repo/first"}}
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(firstRootsReq.ID),
		"result": map[string]any{
			"roots": firstRoots,
		},
	})

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolArtifactSaveText,
			"arguments": map[string]any{"name": "tests/roots-changed-first", "text": "one"},
		},
	})
	firstWrite := h.recv()
	if string(firstWrite.ID) != "2" || firstWrite.Error != nil {
		t.Fatalf("unexpected first write response: %+v", firstWrite)
	}

	normalizedFirst, err := normalizeRootURIs([]string{"file:///repo/first"})
	if err != nil {
		t.Fatalf("normalize first roots: %v", err)
	}
	hashFirst := computeSubspaceHash(normalizedFirst)

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"})
	secondRootsReq := h.recv()
	if secondRootsReq.Method != "roots/list" {
		t.Fatalf("expected second roots/list request, got: %+v", secondRootsReq)
	}

	secondRoots := []map[string]any{{"uri": "file:///repo/second"}}
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(secondRootsReq.ID),
		"result": map[string]any{
			"roots": secondRoots,
		},
	})

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolArtifactSaveText,
			"arguments": map[string]any{"name": "tests/roots-changed-second", "text": "two"},
		},
	})
	secondWrite := h.recv()
	if string(secondWrite.ID) != "3" || secondWrite.Error != nil {
		t.Fatalf("unexpected second write response: %+v", secondWrite)
	}

	normalizedSecond, err := normalizeRootURIs([]string{"file:///repo/second"})
	if err != nil {
		t.Fatalf("normalize second roots: %v", err)
	}
	hashSecond := computeSubspaceHash(normalizedSecond)
	if hashFirst == hashSecond {
		t.Fatalf("expected distinct hashes, got %s", hashFirst)
	}

	firstNames := readIndexNames(t, filepath.Join(storeRoot, hashFirst, "names.json"))
	if _, ok := firstNames["tests/roots-changed-first"]; !ok {
		t.Fatalf("expected first artifact in first subspace index, got: %+v", firstNames)
	}
	if _, ok := firstNames["tests/roots-changed-second"]; ok {
		t.Fatalf("second artifact unexpectedly present in first subspace index: %+v", firstNames)
	}

	secondNames := readIndexNames(t, filepath.Join(storeRoot, hashSecond, "names.json"))
	if _, ok := secondNames["tests/roots-changed-second"]; !ok {
		t.Fatalf("expected second artifact in second subspace index, got: %+v", secondNames)
	}
}

func TestRootsListChangedProtocol_RefreshErrorFallsBackToGlobal(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{name: "method not found", code: -32601},
		{name: "internal error", code: -32603},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storeRoot := t.TempDir()
			h := newProtocolHarness(t, storeRoot)

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"capabilities": map[string]any{
						"roots": map[string]any{"listChanged": true},
					},
				},
			})
			_ = h.recv()

			h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
			firstRootsReq := h.recv()
			if firstRootsReq.Method != "roots/list" {
				t.Fatalf("expected first roots/list request, got: %+v", firstRootsReq)
			}

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(firstRootsReq.ID),
				"result": map[string]any{
					"roots": []map[string]any{{"uri": "file:///repo/first"}},
				},
			})

			h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"})
			secondRootsReq := h.recv()
			if secondRootsReq.Method != "roots/list" {
				t.Fatalf("expected second roots/list request, got: %+v", secondRootsReq)
			}

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(secondRootsReq.ID),
				"error": map[string]any{
					"code":    tc.code,
					"message": "refresh failed",
				},
			})

			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      toolArtifactSaveText,
					"arguments": map[string]any{"name": "tests/roots-refresh-fallback", "text": "ok"},
				},
			})
			writeResp := h.recv()
			if string(writeResp.ID) != "2" || writeResp.Error != nil {
				t.Fatalf("unexpected write response: %+v", writeResp)
			}

			globalNames := readIndexNames(t, filepath.Join(storeRoot, "names.json"))
			if _, ok := globalNames["tests/roots-refresh-fallback"]; !ok {
				t.Fatalf("expected fallback artifact in global index, got: %+v", globalNames)
			}
		})
	}
}

func TestRootsCapabilityProtocol_NonObjectSkipsRootsList(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"roots": true,
			},
		},
	})
	_ = h.recv()

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolArtifactSaveText,
			"arguments": map[string]any{"name": "tests/non-object-roots", "text": "ok"},
		},
	})

	first := h.recv()
	if first.Method == "roots/list" {
		t.Fatalf("unexpected roots/list request for non-object roots capability: %+v", first)
	}
	if string(first.ID) != "2" || first.Error != nil {
		t.Fatalf("unexpected tool response: %+v", first)
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "names.json")); err != nil {
		t.Fatalf("expected global store index to exist: %v", err)
	}
}
