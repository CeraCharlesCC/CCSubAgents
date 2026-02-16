package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync/atomic"
)

func (s *Server) writeResponse(id json.RawMessage, result any, rpcErr *jsonRPCError) error {
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resp.Result = b
	}
	return s.writeJSON(resp)
}

func (s *Server) writeResponseAndLog(method string, id json.RawMessage, result any, rpcErr *jsonRPCError) {
	if err := s.writeResponse(id, result, rpcErr); err != nil {
		log.Printf("event=write_response_failed method=%q error=%q", method, err.Error())
	}
}

func (s *Server) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.enc == nil {
		return errors.New("encoder not initialized")
	}
	return s.enc.Encode(v)
}

func (s *Server) deliverResponse(resp jsonRPCResponse) {
	key := string(resp.ID)
	s.pendingMu.Lock()
	ch, ok := s.pending[key]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

func (s *Server) callClient(ctx context.Context, method string, params any) (json.RawMessage, *jsonRPCError, error) {
	id := atomic.AddInt64(&s.requestID, 1)
	idRaw, err := json.Marshal(id)
	if err != nil {
		return nil, nil, err
	}

	ch := make(chan jsonRPCResponse, 1)
	key := string(idRaw)
	s.pendingMu.Lock()
	s.pending[key] = ch
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, key)
		s.pendingMu.Unlock()
	}()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	if err := s.writeJSON(req); err != nil {
		return nil, nil, err
	}

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error, nil
		}
		return resp.Result, nil, nil
	}
}
