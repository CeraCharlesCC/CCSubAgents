package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"local-artifact-mcp/internal/domain"
	"local-artifact-mcp/internal/infrastructure/filestore"
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
		return
	}

	if envelope.Method == "" && len(envelope.ID) > 0 && (len(envelope.Result) > 0 || envelope.Error != nil) {
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
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
			_ = s.writeResponse(msg.ID, res, rpcErr)
		}
	case "notifications/initialized":
		s.setInitialized(true)
		s.resolveSessionStore(ctx, false)
	case "notifications/roots/list_changed":
		s.resolveSessionStore(ctx, true)
	case "ping":
		if !isNotification {
			_ = s.writeResponse(msg.ID, map[string]any{}, nil)
		}
	case "tools/list":
		res, rpcErr := s.handleToolsList(msg.Params)
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, rpcErr)
		}
	case "tools/call":
		res, rpcErr := s.handleToolsCall(ctx, msg.Params)
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, rpcErr)
		}
	case "resources/list":
		res, rpcErr := s.handleResourcesList(ctx, msg.Params)
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, rpcErr)
		}
	case "resources/read":
		res, rpcErr := s.handleResourcesRead(ctx, msg.Params)
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, rpcErr)
		}
	case "resources/templates/list":
		res := map[string]any{"resourceTemplates": []any{}}
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, nil)
		}
	case "prompts/list":
		res := map[string]any{"prompts": []any{}}
		if !isNotification {
			_ = s.writeResponse(msg.ID, res, nil)
		}
	default:
		if !isNotification {
			_ = s.writeResponse(msg.ID, nil, &jsonRPCError{Code: -32601, Message: "Method not found"})
		}
	}
}

// --- JSON-RPC types ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

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

func (s *Server) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.enc == nil {
		return errors.New("encoder not initialized")
	}
	return s.enc.Encode(v)
}

func bytesTrimNL(b []byte) []byte {
	// Trim trailing \n and optional \r
	b = bytesTrimSuffix(b, '\n')
	b = bytesTrimSuffix(b, '\r')
	return b
}

func bytesTrimSuffix(b []byte, c byte) []byte {
	if len(b) > 0 && b[len(b)-1] == c {
		return b[:len(b)-1]
	}
	return b
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

// --- Lifecycle: initialize ---

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

func (s *Server) resolveSessionStore(ctx context.Context, force bool) {
	s.resolveMu.Lock()
	defer s.resolveMu.Unlock()

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
	if !ok {
		return false
	}
	if rootCap == nil {
		return false
	}
	if _, ok := rootCap.(map[string]any); !ok {
		return false
	}
	return true
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

// --- Tools ---

func (s *Server) handleToolsList(_ json.RawMessage) (any, *jsonRPCError) {
	return map[string]any{"tools": toolDefinitions()}, nil
}

// tools/call params

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolResult struct {
	Content           []any `json:"content"`
	StructuredContent any   `json:"structuredContent,omitempty"`
	IsError           bool  `json:"isError,omitempty"`
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *jsonRPCError) {
	if !s.isInitialized() {
		// Be lenient: some clients call tools before notifications/initialized.
	}

	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return toolError("invalid params"), nil
	}

	// Dispatch
	switch p.Name {
	case toolArtifactSaveText:
		return s.toolSaveText(ctx, p.Arguments)
	case "artifact.save_text":
		return s.toolSaveText(ctx, p.Arguments)
	case toolArtifactSaveBlob:
		return s.toolSaveBlob(ctx, p.Arguments)
	case "artifact.save_blob":
		return s.toolSaveBlob(ctx, p.Arguments)
	case toolArtifactResolve:
		return s.toolResolve(ctx, p.Arguments)
	case "artifact.resolve":
		return s.toolResolve(ctx, p.Arguments)
	case toolArtifactGet:
		return s.toolGet(ctx, p.Arguments)
	case "artifact.get":
		return s.toolGet(ctx, p.Arguments)
	case toolArtifactList:
		return s.toolList(ctx, p.Arguments)
	case "artifact.list":
		return s.toolList(ctx, p.Arguments)
	case toolArtifactDelete:
		return s.toolDelete(ctx, p.Arguments)
	case "artifact.delete":
		return s.toolDelete(ctx, p.Arguments)
	case "deleteArtifact":
		return s.toolDelete(ctx, p.Arguments)
	default:
		return toolError(fmt.Sprintf("unknown tool: %s", p.Name)), nil
	}
}

// --- Tool handlers ---

type saveTextArgs struct {
	Name     string `json:"name"`
	Text     string `json:"text"`
	MimeType string `json:"mimeType,omitempty"`
}

type saveBlobArgs struct {
	Name       string `json:"name"`
	DataBase64 string `json:"dataBase64"`
	MimeType   string `json:"mimeType"`
	Filename   string `json:"filename,omitempty"`
}

type resolveArgs struct {
	Name string `json:"name"`
}

type getArgs struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
	Mode string `json:"mode,omitempty"` // auto|text|resource|image|meta
}

type listArgs struct {
	Prefix string `json:"prefix,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type deleteArgs struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

type saveOut struct {
	Name      string `json:"name"`
	Ref       string `json:"ref"`
	Kind      string `json:"kind"`
	MimeType  string `json:"mimeType"`
	Filename  string `json:"filename,omitempty"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"createdAt"`
	URIByName string `json:"uriByName"`
	URIByRef  string `json:"uriByRef"`
	PrevRef   string `json:"prevRef,omitempty"`
}

func toSaveOut(a domain.Artifact, nameEscaped string) saveOut {
	return saveOut{
		Name:      a.Name,
		Ref:       a.Ref,
		Kind:      string(a.Kind),
		MimeType:  a.MimeType,
		Filename:  a.Filename,
		SizeBytes: a.SizeBytes,
		SHA256:    a.SHA256,
		CreatedAt: a.CreatedAt.Format(time.RFC3339),
		URIByName: domain.URIByName(nameEscaped),
		URIByRef:  a.URIByRef(),
		PrevRef:   a.PrevRef,
	}
}

func (s *Server) toolSaveText(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args saveTextArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {name, text, mimeType?}"), nil
	}
	svc := s.service(ctx)
	a, err := svc.SaveText(ctx, domain.SaveTextInput{Name: args.Name, Text: args.Text, MimeType: args.MimeType})
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	nameEsc := url.PathEscape(a.Name)
	out := toSaveOut(a, nameEsc)
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content: []any{
			textContent("saved"),
			textContent(string(jsonStr)),
			resourceLink(a.Name, domain.URIByName(nameEsc), a.MimeType, a.SizeBytes),
		},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolSaveBlob(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args saveBlobArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {name, dataBase64, mimeType, filename?}"), nil
	}
	data, err := base64.StdEncoding.DecodeString(args.DataBase64)
	if err != nil {
		return toolError("dataBase64 is not valid base64"), nil
	}
	svc := s.service(ctx)
	a, err := svc.SaveBlob(ctx, domain.SaveBlobInput{Name: args.Name, Data: data, MimeType: args.MimeType, Filename: args.Filename})
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	nameEsc := url.PathEscape(a.Name)
	out := toSaveOut(a, nameEsc)
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content: []any{
			textContent("saved"),
			textContent(string(jsonStr)),
			resourceLink(a.Name, domain.URIByName(nameEsc), a.MimeType, a.SizeBytes),
		},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolResolve(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args resolveArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {name}"), nil
	}
	svc := s.service(ctx)
	ref, err := svc.Resolve(ctx, args.Name)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	nameEsc := url.PathEscape(strings.TrimSpace(args.Name))
	out := map[string]any{
		"name":      strings.TrimSpace(args.Name),
		"ref":       ref,
		"uriByName": domain.URIByName(nameEsc),
		"uriByRef":  "artifact://ref/" + ref,
	}
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content:           []any{textContent(string(jsonStr))},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolGet(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args getArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {ref?, name?, mode?}"), nil
	}
	svc := s.service(ctx)
	a, data, err := svc.Get(ctx, domain.Selector{Ref: args.Ref, Name: args.Name})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = modeAuto
	}

	nameEsc := url.PathEscape(a.Name)
	meta := toSaveOut(a, nameEsc)
	metaJSON, _ := json.Marshal(meta)

	// Decide how to return the payload.
	lowerMime := strings.ToLower(a.MimeType)
	isText := strings.HasPrefix(lowerMime, "text/") || a.Kind == domain.ArtifactKindText
	isImage := strings.HasPrefix(lowerMime, "image/") || a.Kind == domain.ArtifactKindImage

	if mode == modeMeta {
		return toolResult{Content: []any{textContent(string(metaJSON))}, StructuredContent: meta}, nil
	}
	if mode == modeAuto {
		if isText {
			mode = modeText
		} else if isImage {
			mode = modeImage
		} else {
			mode = modeResource
		}
	}

	content := make([]any, 0, 3)
	switch mode {
	case modeText:
		content = append(content, textContent(string(data)))
	case modeImage:
		if !isImage {
			// fall back to resource
			content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
		} else {
			content = append(content, imageContent(a.MimeType, data))
		}
	case modeResource:
		content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
	default:
		return toolError("mode must be one of " + modeAuto + "|" + modeText + "|" + modeResource + "|" + modeImage + "|" + modeMeta), nil
	}
	content = append(content, textContent(string(metaJSON)))
	content = append(content, resourceLink(a.Name, meta.URIByName, a.MimeType, a.SizeBytes))

	return toolResult{Content: content, StructuredContent: meta}, nil
}

func (s *Server) toolList(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args listArgs
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return toolError("Invalid arguments: expected {prefix?, limit?}"), nil
		}
	}
	svc := s.service(ctx)
	arts, err := svc.List(ctx, args.Prefix, args.Limit)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	items := make([]saveOut, 0, len(arts))
	for _, a := range arts {
		items = append(items, toSaveOut(a, url.PathEscape(a.Name)))
	}
	out := map[string]any{"items": items}
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content:           []any{textContent(string(jsonStr))},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolDelete(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args deleteArgs
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return toolError("Invalid arguments: expected {ref?, name?}"), nil
		}
	}

	svc := s.service(ctx)
	a, err := svc.Delete(ctx, domain.Selector{Ref: args.Ref, Name: args.Name})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	out := map[string]any{
		"deleted":  true,
		"ref":      a.Ref,
		"uriByRef": "artifact://ref/" + a.Ref,
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		nameEsc := url.PathEscape(name)
		out["name"] = name
		out["uriByName"] = domain.URIByName(nameEsc)
	}
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content: []any{
			textContent("deleted"),
			textContent(string(jsonStr)),
		},
		StructuredContent: out,
	}, nil
}

func toolError(msg string) toolResult {
	return toolResult{Content: []any{textContent(msg)}, IsError: true}
}

func toolErrorFromErr(err error) toolResult {
	if err == nil {
		return toolError("internal error")
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return toolError("not found")
	case errors.Is(err, domain.ErrAliasExists), errors.Is(err, domain.ErrConflict):
		return toolError("conflict: " + err.Error())
	case errors.Is(err, domain.ErrInvalidInput),
		errors.Is(err, domain.ErrNameRequired),
		errors.Is(err, domain.ErrRefRequired),
		errors.Is(err, domain.ErrRefOrName),
		errors.Is(err, domain.ErrRefAndNameMutuallyExclusive),
		errors.Is(err, domain.ErrInvalidName),
		errors.Is(err, domain.ErrInvalidRef),
		errors.Is(err, domain.ErrUnsupportedURI):
		return toolError("invalid input: " + err.Error())
	default:
		return toolError("internal error: " + err.Error())
	}
}

func textContent(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func imageContent(mime string, data []byte) map[string]any {
	return map[string]any{
		"type":     "image",
		"mimeType": mime,
		"data":     base64.StdEncoding.EncodeToString(data),
	}
}

func embeddedBlob(uri, mime string, data []byte) map[string]any {
	return map[string]any{
		"type": "resource",
		"resource": map[string]any{
			"uri":      uri,
			"mimeType": mime,
			"blob":     base64.StdEncoding.EncodeToString(data),
		},
	}
}

func resourceLink(name, uri, mime string, size int64) map[string]any {
	m := map[string]any{
		"type":     "resource_link",
		"name":     name,
		"uri":      uri,
		"mimeType": mime,
		"size":     size,
	}
	return m
}

// --- Resources ---

func (s *Server) handleResourcesList(ctx context.Context, _ json.RawMessage) (any, *jsonRPCError) {
	svc := s.service(ctx)
	arts, err := svc.List(ctx, "", 200)
	if err != nil {
		return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
	}
	resources := make([]map[string]any, 0, len(arts))
	for _, a := range arts {
		nEsc := url.PathEscape(a.Name)
		resources = append(resources, map[string]any{
			"name":        a.Name,
			"uri":         domain.URIByName(nEsc),
			"mimeType":    a.MimeType,
			"description": "Saved artifact",
			"size":        a.SizeBytes,
		})
	}
	return map[string]any{"resources": resources}, nil
}

type readResourceParams struct {
	URI string `json:"uri"`
}

func (s *Server) handleResourcesRead(ctx context.Context, params json.RawMessage) (any, *jsonRPCError) {
	var p readResourceParams
	if err := json.Unmarshal(params, &p); err != nil {
		return resourceReadErrorResult("", "invalid params: expected {uri}"), nil
	}
	uri := strings.TrimSpace(p.URI)
	if uri == "" {
		return resourceReadErrorResult("", "invalid params: uri is required"), nil
	}

	sel, err := selectorFromURI(uri)
	if err != nil {
		if isRecoverableReadErr(err) {
			return resourceReadErrorResult(uri, resourceReadErrorMessage(err)), nil
		}
		return nil, rpcErrorFromErr(err)
	}
	svc := s.service(ctx)
	a, data, err := svc.Get(ctx, sel)
	if err != nil {
		if isRecoverableReadErr(err) {
			return resourceReadErrorResult(uri, resourceReadErrorMessage(err)), nil
		}
		return nil, rpcErrorFromErr(err)
	}

	lowerMime := strings.ToLower(a.MimeType)
	isText := strings.HasPrefix(lowerMime, "text/") || a.Kind == domain.ArtifactKindText
	var contents []map[string]any
	if isText {
		contents = []map[string]any{{
			"uri":      uri,
			"mimeType": a.MimeType,
			"text":     string(data),
		}}
	} else {
		contents = []map[string]any{{
			"uri":      uri,
			"mimeType": a.MimeType,
			"blob":     base64.StdEncoding.EncodeToString(data),
		}}
	}
	return map[string]any{"contents": contents}, nil
}

func resourceReadErrorResult(uri, msg string) map[string]any {
	if strings.TrimSpace(uri) == "" {
		uri = "artifact://error"
	}
	return map[string]any{
		"contents": []map[string]any{{
			"uri":      uri,
			"mimeType": "text/plain",
			"text":     "error: " + msg,
		}},
	}
}

func selectorFromURI(raw string) (domain.Selector, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return domain.Selector{}, fmt.Errorf("%w: invalid uri", domain.ErrInvalidInput)
	}
	if u.Scheme != "artifact" {
		return domain.Selector{}, fmt.Errorf("%w: unsupported uri scheme %q", domain.ErrUnsupportedURI, u.Scheme)
	}
	kind := u.Host
	val := strings.TrimPrefix(u.Path, "/")
	switch kind {
	case "ref":
		if val == "" {
			return domain.Selector{}, fmt.Errorf("%w: ref uri missing value", domain.ErrInvalidInput)
		}
		return domain.Selector{Ref: val}, nil
	case "name":
		if val == "" {
			return domain.Selector{}, fmt.Errorf("%w: name uri missing value", domain.ErrInvalidInput)
		}
		name, err := url.PathUnescape(val)
		if err != nil {
			return domain.Selector{}, fmt.Errorf("%w: invalid name uri encoding", domain.ErrInvalidInput)
		}
		return domain.Selector{Name: name}, nil
	default:
		return domain.Selector{}, fmt.Errorf("%w: unsupported artifact uri host %q", domain.ErrUnsupportedURI, kind)
	}
}

func rpcErrorFromErr(err error) *jsonRPCError {
	switch {
	case isRecoverableReadErr(err):
		return &jsonRPCError{Code: -32602, Message: resourceReadErrorMessage(err)}
	default:
		return &jsonRPCError{Code: -32603, Message: err.Error()}
	}
}

func isRecoverableReadErr(err error) bool {
	return errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrInvalidInput) ||
		errors.Is(err, domain.ErrNameRequired) ||
		errors.Is(err, domain.ErrRefRequired) ||
		errors.Is(err, domain.ErrRefOrName) ||
		errors.Is(err, domain.ErrRefAndNameMutuallyExclusive) ||
		errors.Is(err, domain.ErrInvalidName) ||
		errors.Is(err, domain.ErrInvalidRef) ||
		errors.Is(err, domain.ErrUnsupportedURI)
}

func resourceReadErrorMessage(err error) string {
	if errors.Is(err, domain.ErrNotFound) {
		return "not found"
	}
	return err.Error()
}
