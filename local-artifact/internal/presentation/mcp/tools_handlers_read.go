package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

type resolveArgs struct {
	Name string `json:"name"`
}

type getArgs struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
	Mode string `json:"mode,omitempty"`
}

type listArgs struct {
	Prefix string `json:"prefix,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type deleteArgs struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

func (s *Server) toolResolve(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args resolveArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {name}"), nil
	}

	resolved, err := s.daemon().Resolve(ctx, daemon.ResolveRequest{Workspace: s.currentWorkspace(ctx), Name: args.Name})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	nameEsc := url.PathEscape(strings.TrimSpace(args.Name))
	out := map[string]any{
		"name":      strings.TrimSpace(args.Name),
		"ref":       resolved.Ref,
		"uriByName": artifacts.URIByName(nameEsc),
		"uriByRef":  "artifact://ref/" + resolved.Ref,
	}

	return toolResult{
		Content:           []any{textContent("resolved")},
		StructuredContent: out,
	}, nil
}

func (s *Server) toolGet(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args getArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {ref?, name?, mode?}"), nil
	}

	getOut, err := s.daemon().Get(ctx, daemon.GetRequest{
		Workspace: s.currentWorkspace(ctx),
		Selector:  daemon.Selector{Ref: args.Ref, Name: args.Name},
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	a := getOut.Artifact
	data, err := base64.StdEncoding.DecodeString(getOut.DataBase64)
	if err != nil {
		return toolError("internal error: invalid daemon payload"), nil
	}

	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = modeAuto
	}

	nameEsc := url.PathEscape(a.Name)
	meta := toSaveOut(a, nameEsc)

	lowerMime := strings.ToLower(a.MimeType)
	isText := strings.HasPrefix(lowerMime, "text/") || a.Kind == artifacts.ArtifactKindText
	isImage := strings.HasPrefix(lowerMime, "image/") || a.Kind == artifacts.ArtifactKindImage

	if mode == modeMeta {
		return toolResult{Content: []any{textContent("metadata only")}, StructuredContent: meta}, nil
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
			content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
		} else {
			content = append(content, imageContent(a.MimeType, data))
		}
	case modeResource:
		content = append(content, embeddedBlob(meta.URIByRef, a.MimeType, data))
	default:
		return toolError("mode must be one of " + modeAuto + "|" + modeText + "|" + modeResource + "|" + modeImage + "|" + modeMeta), nil
	}

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

	listOut, err := s.daemon().List(ctx, daemon.ListRequest{Workspace: s.currentWorkspace(ctx), Prefix: args.Prefix, Limit: args.Limit})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	items := make([]saveOut, 0, len(listOut.Items))
	for _, a := range listOut.Items {
		items = append(items, toSaveOut(a, url.PathEscape(a.Name)))
	}
	out := map[string]any{"items": items}

	return toolResult{
		Content:           []any{textContent(fmt.Sprintf("%d artifacts", len(items)))},
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

	deleteOut, err := s.daemon().Delete(ctx, daemon.DeleteRequest{
		Workspace: s.currentWorkspace(ctx),
		Selector:  daemon.Selector{Ref: args.Ref, Name: args.Name},
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	a := deleteOut.Artifact

	out := map[string]any{
		"deleted":  true,
		"ref":      a.Ref,
		"uriByRef": "artifact://ref/" + a.Ref,
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		nameEsc := url.PathEscape(name)
		out["name"] = name
		out["uriByName"] = artifacts.URIByName(nameEsc)
	}

	return toolResult{
		Content: []any{
			textContent("deleted"),
		},
		StructuredContent: out,
	}, nil
}
