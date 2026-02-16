package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
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
