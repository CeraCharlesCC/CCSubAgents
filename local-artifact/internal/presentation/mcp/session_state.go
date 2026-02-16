package mcp

import (
	"context"
	"encoding/json"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      any            `json:"clientInfo"`
}

func (s *Server) handleInitialize(params json.RawMessage) (any, *jsonRPCError) {
	var p initializeParams
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}

	s.sessionMu.Lock()
	s.clientCapabilities = p.Capabilities
	s.sessionMu.Unlock()

	return initializeResponse(), nil
}

func (s *Server) service(ctx context.Context) *domain.Service {
	s.resolveSessionStore(ctx, false)

	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.svc
}

func (s *Server) isInitialized() bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.initialized
}

func (s *Server) setInitialized(v bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.initialized = v
}

func (s *Server) isSessionResolved() bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.sessionResolved
}

func (s *Server) setSessionService(svc *domain.Service, resolved bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.svc = svc
	s.sessionResolved = resolved
}
