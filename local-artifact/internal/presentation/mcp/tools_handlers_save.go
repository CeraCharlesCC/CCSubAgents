package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

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

type saveOut struct {
	Name      string `json:"name"`
	Ref       string `json:"ref"`
	Kind      string `json:"kind"`
	MimeType  string `json:"mimeType"`
	Filename  string `json:"filename,omitempty"`
	URIByName string `json:"uriByName"`
	URIByRef  string `json:"uriByRef"`
	PrevRef   string `json:"prevRef,omitempty"`
}

func toSaveOut(a artifacts.Artifact, nameEscaped string) saveOut {
	return saveOut{
		Name:      a.Name,
		Ref:       a.Ref,
		Kind:      string(a.Kind),
		MimeType:  a.MimeType,
		Filename:  a.Filename,
		URIByName: artifacts.URIByName(nameEscaped),
		URIByRef:  a.URIByRef(),
		PrevRef:   a.PrevRef,
	}
}

func (s *Server) toolSaveText(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args saveTextArgs
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return toolError("Invalid arguments: expected {name, text, mimeType?}"), nil
	}

	a, err := s.daemon().SaveText(ctx, daemon.SaveTextRequest{
		Workspace: s.currentWorkspace(ctx),
		Name:      args.Name,
		Text:      args.Text,
		MimeType:  args.MimeType,
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	nameEsc := url.PathEscape(a.Name)
	out := toSaveOut(a, nameEsc)

	return toolResult{
		Content: []any{
			textContent("saved"),
			resourceLink(a.Name, artifacts.URIByName(nameEsc), a.MimeType, a.SizeBytes),
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

	a, err := s.daemon().SaveBlob(ctx, daemon.SaveBlobRequest{
		Workspace:  s.currentWorkspace(ctx),
		Name:       args.Name,
		DataBase64: base64.StdEncoding.EncodeToString(data),
		MimeType:   args.MimeType,
		Filename:   args.Filename,
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	nameEsc := url.PathEscape(a.Name)
	out := toSaveOut(a, nameEsc)

	return toolResult{
		Content: []any{
			textContent("saved"),
			resourceLink(a.Name, artifacts.URIByName(nameEsc), a.MimeType, a.SizeBytes),
		},
		StructuredContent: out,
	}, nil
}
