package mcp

import (
	"encoding/base64"
	"errors"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

func toolError(msg string) toolResult {
	return toolResult{Content: []any{textContent(msg)}, IsError: true}
}

func toolErrorFromErr(err error) toolResult {
	if err == nil {
		return toolError("internal error")
	}

	var remoteErr *daemon.RemoteError
	if errors.As(err, &remoteErr) {
		switch remoteErr.Code {
		case daemon.CodeNotFound:
			return toolError("not found")
		case daemon.CodeConflict:
			return toolError("conflict: " + remoteErr.Message)
		case daemon.CodeInvalidInput:
			return toolError("invalid input: " + remoteErr.Message)
		case daemon.CodeUnauthorized:
			return toolError("unauthorized: " + remoteErr.Message)
		case daemon.CodeServiceUnavailable:
			return toolError("internal error: service unavailable: " + remoteErr.Message)
		default:
			return toolError("internal error: " + remoteErr.Message)
		}
	}

	switch {
	case errors.Is(err, artifacts.ErrNotFound):
		return toolError("not found")
	case errors.Is(err, artifacts.ErrAliasExists), errors.Is(err, artifacts.ErrConflict):
		return toolError("conflict: " + err.Error())
	case errors.Is(err, artifacts.ErrInvalidInput),
		errors.Is(err, artifacts.ErrNameRequired),
		errors.Is(err, artifacts.ErrRefRequired),
		errors.Is(err, artifacts.ErrRefOrName),
		errors.Is(err, artifacts.ErrRefAndNameMutuallyExclusive),
		errors.Is(err, artifacts.ErrInvalidName),
		errors.Is(err, artifacts.ErrInvalidRef),
		errors.Is(err, artifacts.ErrUnsupportedURI):
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
