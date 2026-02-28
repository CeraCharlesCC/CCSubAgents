package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/jsonbody"
)

type Server struct {
	engine          *Engine
	owner           string
	maxRequestBytes int64
	shutdownFn      func()
	mu              sync.RWMutex
}

func NewServer(engine *Engine, owner string) *Server {
	if strings.TrimSpace(owner) == "" {
		owner = "daemon"
	}
	return &Server{engine: engine, owner: owner, maxRequestBytes: DefaultMaxRequestBytes}
}

func (s *Server) SetShutdownFunc(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownFn = fn
}

func (s *Server) shutdownFunc() func() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutdownFn
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon/v1/health", s.handleHealth)
	mux.HandleFunc("/daemon/v1/control/shutdown", s.handleShutdown)
	mux.HandleFunc("/daemon/v1/artifacts/save_text", s.handleSaveText)
	mux.HandleFunc("/daemon/v1/artifacts/save_blob", s.handleSaveBlob)
	mux.HandleFunc("/daemon/v1/artifacts/resolve", s.handleResolve)
	mux.HandleFunc("/daemon/v1/artifacts/get", s.handleGet)
	mux.HandleFunc("/daemon/v1/artifacts/list", s.handleList)
	mux.HandleFunc("/daemon/v1/artifacts/delete", s.handleDelete)
	return mux
}

func (s *Server) writeOK(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{OK: true, Data: data})
}

func (s *Server) writeErr(w http.ResponseWriter, err error) {
	status, payload := mapCoreError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: payload})
}

func ensurePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	writeMethodNotAllowed(w, http.MethodPost)
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	s.writeOK(w, http.StatusOK, HealthResponse{Status: "ok"})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	s.writeOK(w, http.StatusAccepted, ShutdownResponse{Status: "shutting-down"})
	if fn := s.shutdownFunc(); fn != nil {
		go fn()
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	if strings.TrimSpace(allowed) != "" {
		w.Header().Set("Allow", allowed)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &EnvelopeError{Code: CodeMethodNotAllowed, Message: "method not allowed"}})
}

func (s *Server) resolveService(ctx context.Context, selector WorkspaceSelector) (string, *artifacts.Service, error) {
	return s.engine.resolveWorkspace(ctx, selector, s.owner)
}

func (s *Server) handleSaveText(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req SaveTextRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	a, err := svc.SaveText(r.Context(), artifacts.SaveTextInput{
		Name:            req.Name,
		Text:            req.Text,
		MimeType:        req.MimeType,
		ExpectedPrevRef: req.ExpectedPrevRef,
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, map[string]any{"artifact": a})
}

func (s *Server) handleSaveBlob(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req SaveBlobRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.DataBase64))
	if err != nil {
		s.writeErr(w, fmt.Errorf("%w: dataBase64 is not valid base64", artifacts.ErrInvalidInput))
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	a, err := svc.SaveBlob(r.Context(), artifacts.SaveBlobInput{
		Name:            req.Name,
		Data:            data,
		MimeType:        req.MimeType,
		Filename:        req.Filename,
		ExpectedPrevRef: req.ExpectedPrevRef,
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, map[string]any{"artifact": a})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req ResolveRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	ref, err := svc.Resolve(r.Context(), req.Name)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, ResolveResponse{Name: strings.TrimSpace(req.Name), Ref: ref})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req GetRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	sel := artifacts.Selector{Ref: req.Selector.Ref, Name: req.Selector.Name}
	a, data, err := svc.Get(r.Context(), sel)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, GetResponse{Artifact: a, DataBase64: base64.StdEncoding.EncodeToString(data)})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req ListRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	items, err := svc.List(r.Context(), req.Prefix, req.Limit)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, ListResponse{Items: items})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	var req DeleteRequest
	if err := jsonbody.DecodeStrictJSON(r, s.maxRequestBytes, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	_, svc, err := s.resolveService(r.Context(), req.Workspace)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	a, err := svc.Delete(r.Context(), artifacts.Selector{Ref: req.Selector.Ref, Name: req.Selector.Name})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeOK(w, http.StatusOK, DeleteResponse{Deleted: true, Artifact: a})
}
