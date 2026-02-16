package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/filestore"
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

	hash := computeSubspaceHash(normalized)
	s.setSessionService(domain.NewService(filestore.New(filepath.Join(s.baseStoreRoot, hash))), true)
	log.Printf("event=roots_list_success root_count=%d subspace_hash=%s", len(normalized), hash)
}

func (s *Server) useGlobalFallbackSession(reason string) {
	s.setSessionService(domain.NewService(filestore.New(s.baseStoreRoot)), true)
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

	return normalizeRootURIs(uris)
}

func normalizeRootURIs(roots []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, raw := range roots {
		normalized, err := normalizeRootURI(raw)
		if err != nil {
			continue
		}
		set[normalized] = struct{}{}
	}

	if len(set) == 0 {
		return nil, errors.New("no valid file roots")
	}

	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeRootURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty root uri")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return "", errors.New("root uri must use file scheme")
	}
	if strings.TrimSpace(u.Path) == "" {
		return "", errors.New("root uri path is required")
	}

	u.Scheme = "file"
	host := strings.ToLower(strings.TrimSpace(u.Host))
	if host == "localhost" {
		host = ""
	}
	u.Host = host
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = path.Clean(u.Path)
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawPath = ""

	return u.String(), nil
}

func computeSubspaceHash(normalizedSortedRoots []string) string {
	joined := strings.Join(normalizedSortedRoots, "\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}
