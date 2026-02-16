package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

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
