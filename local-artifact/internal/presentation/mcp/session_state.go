package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
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

func (s *Server) currentWorkspace(ctx context.Context) daemon.WorkspaceSelector {
	s.resolveSessionStore(ctx, false)

	s.sessionMu.RLock()
	current := s.workspace
	s.sessionMu.RUnlock()
	if current.WorkspaceID != "" || len(current.Roots) > 0 {
		return current
	}
	return daemon.WorkspaceSelector{WorkspaceID: workspaces.GlobalWorkspaceID}
}

func (s *Server) daemon() *daemon.Client {
	s.sessionMu.RLock()
	client := s.daemonClient
	s.sessionMu.RUnlock()
	if client != nil {
		return client
	}
	return daemon.NewUnavailableClient(errors.New("daemon client unavailable"))
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

func (s *Server) setSessionWorkspace(workspace daemon.WorkspaceSelector, resolved bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.workspace = workspace
	s.sessionResolved = resolved
}
