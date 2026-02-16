package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/filestore"
)

type Server struct {
	baseStoreRoot string

	sessionMu sync.RWMutex
	svc       *domain.Service

	initialized        bool
	clientCapabilities map[string]any
	sessionResolved    bool

	resolveMu sync.Mutex

	enc     *json.Encoder
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan jsonRPCResponse
	requestID int64
}

func New(baseStoreRoot string) *Server {
	globalRepo := filestore.New(baseStoreRoot)
	return &Server{
		baseStoreRoot: baseStoreRoot,
		svc:           domain.NewService(globalRepo),
		pending:       map[string]chan jsonRPCResponse{},
	}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	s.enc = enc

	r := bufio.NewReader(in)
	reqCh := make(chan jsonRPCRequest, 16)
	errCh := make(chan error, 1)
	go s.readLoop(ctx, r, reqCh, errCh)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err == nil || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case msg := <-reqCh:
			s.handleRequest(ctx, msg)
		}
	}
}

func (s *Server) readLoop(ctx context.Context, r *bufio.Reader, reqCh chan<- jsonRPCRequest, errCh chan<- error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			errCh <- err
			return
		}

		if len(line) > 0 {
			line = bytesTrimNL(line)
			s.handleIncomingLine(ctx, line, reqCh)
		}

		if errors.Is(err, io.EOF) {
			errCh <- nil
			return
		}
	}
}

func (s *Server) handleIncomingLine(ctx context.Context, line []byte, reqCh chan<- jsonRPCRequest) {
	var envelope struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *jsonRPCError   `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		log.Printf("event=incoming_unmarshal_failed stage=envelope error=%q", err.Error())
		return
	}

	if envelope.Method == "" && len(envelope.ID) > 0 && (len(envelope.Result) > 0 || envelope.Error != nil) {
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			log.Printf("event=incoming_unmarshal_failed stage=response error=%q", err.Error())
			return
		}
		s.deliverResponse(resp)
		return
	}

	if envelope.Method == "" {
		return
	}

	var msg jsonRPCRequest
	if err := json.Unmarshal(line, &msg); err != nil {
		log.Printf("event=incoming_unmarshal_failed stage=request error=%q", err.Error())
		return
	}

	select {
	case <-ctx.Done():
		return
	case reqCh <- msg:
	}
}

func (s *Server) handleRequest(ctx context.Context, msg jsonRPCRequest) {
	isNotification := len(msg.ID) == 0

	switch msg.Method {
	case "initialize":
		res, rpcErr := s.handleInitialize(msg.Params)
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, rpcErr)
		}
	case "notifications/initialized":
		s.setInitialized(true)
		s.resolveSessionStore(ctx, false)
	case "notifications/roots/list_changed":
		s.resolveSessionStore(ctx, true)
	case "ping":
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, map[string]any{}, nil)
		}
	case "tools/list":
		res, rpcErr := s.handleToolsList(msg.Params)
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, rpcErr)
		}
	case "tools/call":
		res, rpcErr := s.handleToolsCall(ctx, msg.Params)
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, rpcErr)
		}
	case "resources/list":
		res, rpcErr := s.handleResourcesList(ctx, msg.Params)
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, rpcErr)
		}
	case "resources/read":
		res, rpcErr := s.handleResourcesRead(ctx, msg.Params)
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, rpcErr)
		}
	case "resources/templates/list":
		res := map[string]any{"resourceTemplates": []any{}}
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, nil)
		}
	case "prompts/list":
		res := map[string]any{"prompts": []any{}}
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, res, nil)
		}
	default:
		if !isNotification {
			s.writeResponseAndLog(msg.Method, msg.ID, nil, &jsonRPCError{Code: -32601, Message: "Method not found"})
		}
	}
}
