package mcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

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

const (
	protocolRecvTimeout     = 6 * time.Second
	protocolShutdownTimeout = 6 * time.Second
)

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

	s := newDaemonBackedServerAtRoot(t, baseStoreRoot)
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
	case <-time.After(protocolShutdownTimeout):
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
	case <-time.After(protocolRecvTimeout):
		h.t.Fatal("timed out waiting for server output")
	}
	return protocolMessage{}
}

func (h *protocolHarness) initializeRootsCap(listChanged bool) protocolMessage {
	h.t.Helper()
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": listChanged},
			},
		},
	})
	return h.recv()
}

func (h *protocolHarness) notifyInitialized() {
	h.t.Helper()
	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (h *protocolHarness) expectRootsListRequest() json.RawMessage {
	h.t.Helper()
	msg := h.recv()
	if msg.Method != "roots/list" {
		h.t.Fatalf("expected roots/list request, got: %+v", msg)
	}
	if len(msg.ID) == 0 {
		h.t.Fatalf("expected roots/list request id, got: %+v", msg)
	}
	return msg.ID
}

func (h *protocolHarness) replyRootsList(id json.RawMessage, roots []map[string]any) {
	h.t.Helper()
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  map[string]any{"roots": roots},
	})
}

func (h *protocolHarness) replyRootsListError(id json.RawMessage, code int) {
	h.t.Helper()
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": "simulated",
		},
	})
}

func (h *protocolHarness) callTool(id int, name string, args map[string]any) protocolMessage {
	h.t.Helper()
	h.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	return h.recv()
}

func readActiveArtifactNames(t *testing.T, dbPath string) map[string]struct{} {
	t.Helper()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return map[string]struct{}{}
	}
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open db %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.Query(`SELECT name FROM artifacts WHERE deleted = 0;`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table: artifacts") {
			return map[string]struct{}{}
		}
		t.Fatalf("query artifacts in %s: %v", dbPath, err)
	}
	defer rows.Close()

	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan artifact name from %s: %v", dbPath, err)
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate artifact names from %s: %v", dbPath, err)
	}
	return out
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

	initializeResp := h.initializeRootsCap(true)
	if string(initializeResp.ID) != "1" || initializeResp.Error != nil {
		t.Fatalf("unexpected initialize response: %+v", initializeResp)
	}

	h.notifyInitialized()
	rootsID := h.expectRootsListRequest()

	roots := []map[string]any{{"uri": "file:///repo/a"}, {"uri": "file://localhost/repo/b/../b"}}
	h.replyRootsList(rootsID, roots)

	toolResp := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/protocol-success", "text": "ok"})
	if string(toolResp.ID) != "2" || toolResp.Error != nil {
		t.Fatalf("unexpected tool response: %+v", toolResp)
	}

	normalized, err := normalizeRootURIs([]string{"file:///repo/a", "file://localhost/repo/b/../b"})
	if err != nil {
		t.Fatalf("normalize roots: %v", err)
	}
	hash := computeSubspaceHash(normalized)

	hashDB := filepath.Join(storeRoot, hash, "meta.sqlite")
	if _, err := os.Stat(hashDB); err != nil {
		t.Fatalf("expected scoped store sqlite metadata to exist: %v", err)
	}
	hashNames := readActiveArtifactNames(t, hashDB)
	if _, ok := hashNames["tests/protocol-success"]; !ok {
		t.Fatalf("expected scoped sqlite metadata to include artifact, got: %+v", hashNames)
	}

	globalNames := readActiveArtifactNames(t, filepath.Join(storeRoot, "meta.sqlite"))
	if _, ok := globalNames["tests/protocol-success"]; ok {
		t.Fatalf("expected scoped write not to appear in global metadata")
	}
}

func TestWorkspaceHashOverrideProtocol_UsesOverrideAndSkipsRootsList(t *testing.T) {
	storeRoot := t.TempDir()
	overrideHash := strings.Repeat("d", 64)
	t.Setenv(workspaceHashOverrideEnv, strings.ToUpper(overrideHash))

	h := newProtocolHarness(t, storeRoot)

	initializeResp := h.initializeRootsCap(true)
	if string(initializeResp.ID) != "1" || initializeResp.Error != nil {
		t.Fatalf("unexpected initialize response: %+v", initializeResp)
	}

	h.notifyInitialized()
	toolResp := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/override-hash", "text": "ok"})
	if toolResp.Method == "roots/list" {
		t.Fatalf("unexpected roots/list request when workspace override is set: %+v", toolResp)
	}
	if string(toolResp.ID) != "2" || toolResp.Error != nil {
		t.Fatalf("unexpected tool response: %+v", toolResp)
	}

	overrideNames := readActiveArtifactNames(t, filepath.Join(storeRoot, overrideHash, "meta.sqlite"))
	if _, ok := overrideNames["tests/override-hash"]; !ok {
		t.Fatalf("expected override hash sqlite metadata to include artifact, got: %+v", overrideNames)
	}

	globalNames := readActiveArtifactNames(t, filepath.Join(storeRoot, "meta.sqlite"))
	if _, ok := globalNames["tests/override-hash"]; ok {
		t.Fatalf("expected override hash write not to appear in global metadata")
	}
}

func TestRootsListProtocol_DefersRootsListUntilInitializedNotification(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	initializeResp := h.initializeRootsCap(true)
	if string(initializeResp.ID) != "1" || initializeResp.Error != nil {
		t.Fatalf("unexpected initialize response: %+v", initializeResp)
	}

	preInitResp := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/pre-init", "text": "ok"})
	if preInitResp.Method == "roots/list" {
		t.Fatalf("unexpected roots/list before initialized notification: %+v", preInitResp)
	}
	if string(preInitResp.ID) != "2" || preInitResp.Error != nil {
		t.Fatalf("unexpected pre-init tool response: %+v", preInitResp)
	}

	h.notifyInitialized()
	rootsReqID := h.expectRootsListRequest()
	if len(rootsReqID) == 0 {
		t.Fatal("expected non-empty roots/list request id after initialized notification")
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

			_ = h.initializeRootsCap(true)
			h.notifyInitialized()
			rootsID := h.expectRootsListRequest()
			h.replyRootsListError(rootsID, tc.code)

			toolResp := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/protocol-fallback", "text": "ok"})
			if string(toolResp.ID) != "2" || toolResp.Error != nil {
				t.Fatalf("unexpected tool response: %+v", toolResp)
			}

			globalNames := readActiveArtifactNames(t, filepath.Join(storeRoot, "meta.sqlite"))
			if _, ok := globalNames["tests/protocol-fallback"]; !ok {
				t.Fatalf("expected global fallback artifact in sqlite metadata, got: %+v", globalNames)
			}
		})
	}
}

func TestRootsListChangedProtocol_ReResolvesToNewScopedStore(t *testing.T) {
	storeRoot := t.TempDir()
	h := newProtocolHarness(t, storeRoot)

	_ = h.initializeRootsCap(true)
	h.notifyInitialized()
	firstRootsID := h.expectRootsListRequest()

	firstRoots := []map[string]any{{"uri": "file:///repo/first"}}
	h.replyRootsList(firstRootsID, firstRoots)

	firstWrite := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/roots-changed-first", "text": "one"})
	if string(firstWrite.ID) != "2" || firstWrite.Error != nil {
		t.Fatalf("unexpected first write response: %+v", firstWrite)
	}

	normalizedFirst, err := normalizeRootURIs([]string{"file:///repo/first"})
	if err != nil {
		t.Fatalf("normalize first roots: %v", err)
	}
	hashFirst := computeSubspaceHash(normalizedFirst)

	h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"})
	secondRootsID := h.expectRootsListRequest()

	secondRoots := []map[string]any{{"uri": "file:///repo/second"}}
	h.replyRootsList(secondRootsID, secondRoots)

	secondWrite := h.callTool(3, toolArtifactSaveText, map[string]any{"name": "tests/roots-changed-second", "text": "two"})
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

	firstNames := readActiveArtifactNames(t, filepath.Join(storeRoot, hashFirst, "meta.sqlite"))
	if _, ok := firstNames["tests/roots-changed-first"]; !ok {
		t.Fatalf("expected first artifact in first subspace metadata, got: %+v", firstNames)
	}
	if _, ok := firstNames["tests/roots-changed-second"]; ok {
		t.Fatalf("second artifact unexpectedly present in first subspace metadata: %+v", firstNames)
	}

	secondNames := readActiveArtifactNames(t, filepath.Join(storeRoot, hashSecond, "meta.sqlite"))
	if _, ok := secondNames["tests/roots-changed-second"]; !ok {
		t.Fatalf("expected second artifact in second subspace metadata, got: %+v", secondNames)
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

			_ = h.initializeRootsCap(true)
			h.notifyInitialized()
			firstRootsID := h.expectRootsListRequest()
			h.replyRootsList(firstRootsID, []map[string]any{{"uri": "file:///repo/first"}})

			h.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"})
			secondRootsID := h.expectRootsListRequest()
			h.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(secondRootsID),
				"error": map[string]any{
					"code":    tc.code,
					"message": "refresh failed",
				},
			})

			writeResp := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/roots-refresh-fallback", "text": "ok"})
			if string(writeResp.ID) != "2" || writeResp.Error != nil {
				t.Fatalf("unexpected write response: %+v", writeResp)
			}

			globalNames := readActiveArtifactNames(t, filepath.Join(storeRoot, "meta.sqlite"))
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
	first := h.callTool(2, toolArtifactSaveText, map[string]any{"name": "tests/non-object-roots", "text": "ok"})
	if first.Method == "roots/list" {
		t.Fatalf("unexpected roots/list request for non-object roots capability: %+v", first)
	}
	if string(first.ID) != "2" || first.Error != nil {
		t.Fatalf("unexpected tool response: %+v", first)
	}
	globalNames := readActiveArtifactNames(t, filepath.Join(storeRoot, "meta.sqlite"))
	if _, ok := globalNames["tests/non-object-roots"]; !ok {
		t.Fatalf("expected global fallback artifact in sqlite metadata, got: %+v", globalNames)
	}
}
