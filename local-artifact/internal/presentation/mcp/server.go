package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"local-artifact-mcp/internal/domain"
)

const ProtocolVersion = "2025-11-25"

type Server struct {
	svc *domain.Service

	initialized bool
}

func New(svc *domain.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return nil
			}
			continue
		}
		line = bytesTrimNL(line)

		var msg jsonRPCRequest
		if e := json.Unmarshal(line, &msg); e != nil {
			// Malformed JSON-RPC message; can't respond w/o id.
			continue
		}

		// Notifications have no id. Requests have id.
		isNotification := len(msg.ID) == 0

		switch msg.Method {
		case "initialize":
			res, rpcErr := s.handleInitialize(msg.Params)
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, rpcErr)
			}
		case "notifications/initialized":
			s.initialized = true
			// no response
		case "ping":
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, map[string]any{}, nil)
			}
		case "tools/list":
			res, rpcErr := s.handleToolsList(msg.Params)
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, rpcErr)
			}
		case "tools/call":
			res, rpcErr := s.handleToolsCall(ctx, msg.Params)
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, rpcErr)
			}
		case "resources/list":
			res, rpcErr := s.handleResourcesList(ctx, msg.Params)
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, rpcErr)
			}
		case "resources/read":
			res, rpcErr := s.handleResourcesRead(ctx, msg.Params)
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, rpcErr)
			}
		case "resources/templates/list":
			res := map[string]any{"resourceTemplates": []any{}}
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, nil)
			}
		case "prompts/list":
			res := map[string]any{"prompts": []any{}}
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, res, nil)
			}
		default:
			if !isNotification {
				_ = s.writeResponse(enc, msg.ID, nil, &jsonRPCError{Code: -32601, Message: "Method not found"})
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// --- JSON-RPC types ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) writeResponse(enc *json.Encoder, id json.RawMessage, result any, rpcErr *jsonRPCError) error {
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}
	return enc.Encode(resp)
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

	// Version negotiation: reply with our supported version.
	serverInfo := map[string]any{
		"name":        "local_artifact_store",
		"title":       "Local Artifact Store",
		"version":     "0.1.0",
		"description": "Completely local MCP server that lets agents save and retrieve named artifacts (text, files, images).",
	}

	res := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
			"resources": map[string]any{
				"subscribe":    false,
				"listChanged":  false,
			},
		},
		"serverInfo": serverInfo,
		"instructions": "Use artifact.save_text or artifact.save_blob to persist an artifact under a name. Use artifact.get with name or ref to retrieve. For very large blobs, prefer using resources/read via the artifact:// URI.",
	}

	_ = p // currently unused (capabilities)

	return res, nil
}

// --- Tools ---

type toolDef struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func (s *Server) handleToolsList(_ json.RawMessage) (any, *jsonRPCError) {
	tools := []toolDef{
		{
			Name:        "artifact.save_text",
			Title:       "Save text artifact",
			Description: "Save UTF-8 text under a name and return a stable ref and artifact:// URIs.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name":     map[string]any{"type": "string", "description": "Artifact name/alias (e.g. plan/task-123)."},
					"text":     map[string]any{"type": "string", "description": "Text content to save."},
					"mimeType": map[string]any{"type": "string", "description": "Optional MIME type. Defaults to text/plain; charset=utf-8."},
				},
				"required": []string{"name", "text"},
			},
			OutputSchema: saveOutputSchema(),
			Annotations: map[string]any{"readOnlyHint": false},
		},
		{
			Name:        "artifact.save_blob",
			Title:       "Save file/image artifact",
			Description: "Save a binary blob (e.g., a text file or image) under a name. Data is base64 encoded.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name":       map[string]any{"type": "string", "description": "Artifact name/alias."},
					"dataBase64": map[string]any{"type": "string", "description": "Base64-encoded bytes."},
					"mimeType":   map[string]any{"type": "string", "description": "MIME type (e.g., image/png, application/pdf, text/markdown)."},
					"filename":   map[string]any{"type": "string", "description": "Optional original filename."},
				},
				"required": []string{"name", "dataBase64", "mimeType"},
			},
			OutputSchema: saveOutputSchema(),
			Annotations: map[string]any{"readOnlyHint": false},
		},
		{
			Name:        "artifact.resolve",
			Title:       "Resolve name to ref",
			Description: "Given a name, return the latest ref and URIs without loading the artifact body.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"ref":  map[string]any{"type": "string"},
					"uriByName": map[string]any{"type": "string"},
					"uriByRef":  map[string]any{"type": "string"},
				},
				"required": []string{"name", "ref", "uriByName", "uriByRef"},
			},
			Annotations: map[string]any{"readOnlyHint": true},
		},
		{
			Name:        "artifact.get",
			Title:       "Get artifact",
			Description: "Fetch an artifact by ref or name. For binary, returns embedded resource (base64) unless mode=image.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"ref":  map[string]any{"type": "string"},
					"name": map[string]any{"type": "string"},
					"mode": map[string]any{"type": "string", "enum": []string{"auto", "text", "resource", "image", "meta"}, "description": "auto=text for text/*, else resource"},
				},
			},
			OutputSchema: map[string]any{"type": "object"},
			Annotations: map[string]any{"readOnlyHint": true},
		},
		{
			Name:        "artifact.list",
			Title:       "List artifacts",
			Description: "List latest artifacts by name prefix.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"prefix": map[string]any{"type": "string", "description": "Optional name prefix filter."},
					"limit":  map[string]any{"type": "integer", "description": "Max results (default 200)."},
				},
			},
			OutputSchema: map[string]any{"type": "object"},
			Annotations: map[string]any{"readOnlyHint": true},
		},
	}

	return map[string]any{"tools": tools}, nil
}

func saveOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"ref":       map[string]any{"type": "string"},
			"kind":      map[string]any{"type": "string"},
			"mimeType":  map[string]any{"type": "string"},
			"filename":  map[string]any{"type": "string"},
			"sizeBytes": map[string]any{"type": "integer"},
			"sha256":    map[string]any{"type": "string"},
			"createdAt": map[string]any{"type": "string"},
			"uriByName": map[string]any{"type": "string"},
			"uriByRef":  map[string]any{"type": "string"},
		},
		"required": []string{"name", "ref", "kind", "mimeType", "sizeBytes", "sha256", "createdAt", "uriByName", "uriByRef"},
	}
}

// tools/call params

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolResult struct {
	Content          []any `json:"content"`
	StructuredContent any  `json:"structuredContent,omitempty"`
	IsError          bool  `json:"isError,omitempty"`
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *jsonRPCError) {
	if !s.initialized {
		// Be lenient: some clients call tools before notifications/initialized.
	}

	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "Invalid params"}
	}

	// Dispatch
	switch p.Name {
	case "artifact.save_text":
		return s.toolSaveText(ctx, p.Arguments)
	case "artifact.save_blob":
		return s.toolSaveBlob(ctx, p.Arguments)
	case "artifact.resolve":
		return s.toolResolve(ctx, p.Arguments)
	case "artifact.get":
		return s.toolGet(ctx, p.Arguments)
	case "artifact.list":
		return s.toolList(ctx, p.Arguments)
	default:
		return nil, &jsonRPCError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", p.Name)}
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
		CreatedAt: a.CreatedAt.Format(time.RFC3339Nano),
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
	a, err := s.svc.SaveText(ctx, domain.SaveTextInput{Name: args.Name, Text: args.Text, MimeType: args.MimeType})
	if err != nil {
		return toolError(err.Error()), nil
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
	a, err := s.svc.SaveBlob(ctx, domain.SaveBlobInput{Name: args.Name, Data: data, MimeType: args.MimeType, Filename: args.Filename})
	if err != nil {
		return toolError(err.Error()), nil
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
	ref, err := s.svc.Resolve(ctx, args.Name)
	if err != nil {
		return toolError(err.Error()), nil
	}
	nameEsc := url.PathEscape(strings.TrimSpace(args.Name))
	out := map[string]any{
		"name":     strings.TrimSpace(args.Name),
		"ref":      ref,
		"uriByName": domain.URIByName(nameEsc),
		"uriByRef":  "artifact://ref/" + ref,
	}
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content: []any{textContent(string(jsonStr))},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolGet(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args getArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {ref?, name?, mode?}"), nil
	}
	a, data, err := s.svc.Get(ctx, domain.Selector{Ref: args.Ref, Name: args.Name})
	if err != nil {
		return toolError(err.Error()), nil
	}

	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "auto"
	}

	nameEsc := url.PathEscape(a.Name)
	meta := toSaveOut(a, nameEsc)
	metaJSON, _ := json.Marshal(meta)

	// Decide how to return the payload.
	lowerMime := strings.ToLower(a.MimeType)
	isText := strings.HasPrefix(lowerMime, "text/") || a.Kind == domain.ArtifactKindText
	isImage := strings.HasPrefix(lowerMime, "image/") || a.Kind == domain.ArtifactKindImage

	if mode == "meta" {
		return toolResult{Content: []any{textContent(string(metaJSON))}, StructuredContent: meta}, nil
	}
	if mode == "auto" {
		if isText {
			mode = "text"
		} else if isImage {
			mode = "image"
		} else {
			mode = "resource"
		}
	}

	content := make([]any, 0, 3)
	switch mode {
	case "text":
		content = append(content, textContent(string(data)))
	case "image":
		if !isImage {
			// fall back to resource
			content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
		} else {
			content = append(content, imageContent(a.MimeType, data))
		}
	case "resource":
		content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
	default:
		return toolError("mode must be one of auto|text|resource|image|meta"), nil
	}
	content = append(content, textContent(string(metaJSON)))
	content = append(content, resourceLink(a.Name, meta.URIByName, a.MimeType, a.SizeBytes))

	return toolResult{Content: content, StructuredContent: meta}, nil
}

func (s *Server) toolList(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args listArgs
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &args)
	}
	arts, err := s.svc.List(ctx, args.Prefix, args.Limit)
	if err != nil {
		return toolError(err.Error()), nil
	}
	items := make([]saveOut, 0, len(arts))
	for _, a := range arts {
		items = append(items, toSaveOut(a, url.PathEscape(a.Name)))
	}
	out := map[string]any{"items": items}
	jsonStr, _ := json.Marshal(out)
	return toolResult{
		Content: []any{textContent(string(jsonStr))},
		StructuredContent: out,
	}, nil
}

func toolError(msg string) toolResult {
	return toolResult{Content: []any{textContent(msg)}, IsError: true}
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
	arts, err := s.svc.List(ctx, "", 200)
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
		return nil, &jsonRPCError{Code: -32602, Message: "Invalid params"}
	}
	uri := strings.TrimSpace(p.URI)
	if uri == "" {
		return nil, &jsonRPCError{Code: -32602, Message: "uri is required"}
	}

	sel, err := selectorFromURI(uri)
	if err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: err.Error()}
	}
	a, data, err := s.svc.Get(ctx, sel)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, &jsonRPCError{Code: -32602, Message: "not found"}
		}
		return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
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

func selectorFromURI(raw string) (domain.Selector, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return domain.Selector{}, fmt.Errorf("invalid uri")
	}
	if u.Scheme != "artifact" {
		return domain.Selector{}, fmt.Errorf("unsupported uri scheme")
	}
	kind := u.Host
	val := strings.TrimPrefix(u.Path, "/")
	switch kind {
	case "ref":
		if val == "" {
			return domain.Selector{}, fmt.Errorf("ref uri missing value")
		}
		return domain.Selector{Ref: val}, nil
	case "name":
		if val == "" {
			return domain.Selector{}, fmt.Errorf("name uri missing value")
		}
		name, err := url.PathUnescape(val)
		if err != nil {
			name = val
		}
		return domain.Selector{Name: name}, nil
	default:
		return domain.Selector{}, fmt.Errorf("unsupported artifact uri host")
	}
}

