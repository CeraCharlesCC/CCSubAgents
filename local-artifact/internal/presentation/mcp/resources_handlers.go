package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func (s *Server) handleResourcesList(ctx context.Context, _ json.RawMessage) (any, *jsonRPCError) {
	listOut, err := s.daemon().List(ctx, daemon.ListRequest{Workspace: s.currentWorkspace(ctx), Limit: 200})
	if err != nil {
		return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
	}

	resources := make([]map[string]any, 0, len(listOut.Items))
	for _, a := range listOut.Items {
		nEsc := url.PathEscape(a.Name)
		resources = append(resources, map[string]any{
			"name":        a.Name,
			"uri":         artifacts.URIByName(nEsc),
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

	got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Ref: sel.Ref, Name: sel.Name}})
	if err != nil {
		if isRecoverableReadErr(err) {
			return resourceReadErrorResult(uri, resourceReadErrorMessage(err)), nil
		}
		return nil, rpcErrorFromErr(err)
	}
	a := got.Artifact
	data, err := base64.StdEncoding.DecodeString(got.DataBase64)
	if err != nil {
		return nil, &jsonRPCError{Code: -32603, Message: "invalid daemon payload"}
	}

	lowerMime := strings.ToLower(a.MimeType)
	isText := strings.HasPrefix(lowerMime, "text/") || a.Kind == artifacts.ArtifactKindText

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

func selectorFromURI(raw string) (artifacts.Selector, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return artifacts.Selector{}, fmt.Errorf("%w: invalid uri", artifacts.ErrInvalidInput)
	}
	if u.Scheme != "artifact" {
		return artifacts.Selector{}, fmt.Errorf("%w: unsupported uri scheme %q", artifacts.ErrUnsupportedURI, u.Scheme)
	}

	kind := u.Host
	val := strings.TrimPrefix(u.Path, "/")
	switch kind {
	case "ref":
		if val == "" {
			return artifacts.Selector{}, fmt.Errorf("%w: ref uri missing value", artifacts.ErrInvalidInput)
		}
		return artifacts.Selector{Ref: val}, nil
	case "name":
		if val == "" {
			return artifacts.Selector{}, fmt.Errorf("%w: name uri missing value", artifacts.ErrInvalidInput)
		}
		name, err := url.PathUnescape(val)
		if err != nil {
			return artifacts.Selector{}, fmt.Errorf("%w: invalid name uri encoding", artifacts.ErrInvalidInput)
		}
		return artifacts.Selector{Name: name}, nil
	default:
		return artifacts.Selector{}, fmt.Errorf("%w: unsupported artifact uri host %q", artifacts.ErrUnsupportedURI, kind)
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
	var remoteErr *daemon.RemoteError
	if errors.As(err, &remoteErr) {
		return remoteErr.Code == daemon.CodeNotFound || remoteErr.Code == daemon.CodeInvalidInput
	}

	return errors.Is(err, artifacts.ErrNotFound) ||
		errors.Is(err, artifacts.ErrInvalidInput) ||
		errors.Is(err, artifacts.ErrNameRequired) ||
		errors.Is(err, artifacts.ErrRefRequired) ||
		errors.Is(err, artifacts.ErrRefOrName) ||
		errors.Is(err, artifacts.ErrRefAndNameMutuallyExclusive) ||
		errors.Is(err, artifacts.ErrInvalidName) ||
		errors.Is(err, artifacts.ErrInvalidRef) ||
		errors.Is(err, artifacts.ErrUnsupportedURI)
}

func resourceReadErrorMessage(err error) string {
	var remoteErr *daemon.RemoteError
	if errors.As(err, &remoteErr) {
		if remoteErr.Code == daemon.CodeNotFound {
			return "not found"
		}
		return remoteErr.Message
	}

	if errors.Is(err, artifacts.ErrNotFound) {
		return "not found"
	}
	return err.Error()
}
