package mcp

import (
	"encoding/base64"
	"errors"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

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
	return map[string]any{
		"type":     "resource_link",
		"name":     name,
		"uri":      uri,
		"mimeType": mime,
		"size":     size,
	}
}
