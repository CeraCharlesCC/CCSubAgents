package mcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
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
