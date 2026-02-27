package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func (s *Server) resolveSessionStore(ctx context.Context, force bool) {
	s.resolveMu.Lock()
	defer s.resolveMu.Unlock()

	if !s.isInitialized() {
		return
	}

	if !force && s.isSessionResolved() {
		return
	}

	if !s.clientSupportsRoots() {
		s.useGlobalFallbackSession("roots capability unavailable")
		return
	}

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resultRaw, rpcErr, err := s.callClient(callCtx, "roots/list", nil)
	if err != nil {
		s.useGlobalFallbackSession("roots/list request failed: " + err.Error())
		return
	}
	if rpcErr != nil {
		if rpcErr.Code == -32601 || rpcErr.Code == -32603 {
			s.useGlobalFallbackSession(fmt.Sprintf("roots/list returned code=%d message=%s", rpcErr.Code, rpcErr.Message))
			return
		}
		s.useGlobalFallbackSession(fmt.Sprintf("roots/list returned error code=%d message=%s", rpcErr.Code, rpcErr.Message))
		return
	}

	normalized, err := normalizeRootURIsFromResult(resultRaw)
	if err != nil {
		s.useGlobalFallbackSession("roots/list parse failed: " + err.Error())
		return
	}

	workspaceID := computeSubspaceHash(normalized)
	s.setSessionWorkspace(daemon.WorkspaceSelector{Roots: normalized}, true)
	log.Printf("event=roots_list_success root_count=%d subspace_hash=%s", len(normalized), workspaceID)
}

func (s *Server) useGlobalFallbackSession(reason string) {
	s.setSessionWorkspace(daemon.WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID}, true)
	log.Printf("event=roots_list_fallback fallback=global scope=session reason=%q", reason)
}

func (s *Server) clientSupportsRoots() bool {
	s.sessionMu.RLock()
	caps := s.clientCapabilities
	s.sessionMu.RUnlock()

	if len(caps) == 0 {
		return false
	}

	rootCap, ok := caps["roots"]
	if !ok || rootCap == nil {
		return false
	}

	_, ok = rootCap.(map[string]any)
	return ok
}

func normalizeRootURIsFromResult(resultRaw json.RawMessage) ([]string, error) {
	type root struct {
		URI string `json:"uri"`
	}
	type rootsListResult struct {
		Roots []root `json:"roots"`
	}

	var result rootsListResult
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, err
	}

	uris := make([]string, 0, len(result.Roots))
	for _, r := range result.Roots {
		uris = append(uris, r.URI)
	}

	return workspaces.NormalizeRootURIs(uris)
}

func normalizeRootURIs(roots []string) ([]string, error) {
	return workspaces.NormalizeRootURIs(roots)
}

func normalizeRootURI(raw string) (string, error) {
	return workspaces.NormalizeRootURI(raw)
}

func computeSubspaceHash(normalizedSortedRoots []string) string {
	return workspaces.ComputeWorkspaceID(normalizedSortedRoots)
}
