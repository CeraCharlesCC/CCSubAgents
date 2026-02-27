package daemon

import (
	"errors"
	"net/http"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

const (
	CodeInvalidInput       = "INVALID_INPUT"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeInternal           = "INTERNAL"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

type Envelope struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *EnvelopeError `json:"error,omitempty"`
}

type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func mapCoreError(err error) (int, *EnvelopeError) {
	switch {
	case err == nil:
		return http.StatusOK, nil
	case errors.Is(err, artifacts.ErrNotFound):
		return http.StatusNotFound, &EnvelopeError{Code: CodeNotFound, Message: err.Error()}
	case errors.Is(err, artifacts.ErrAliasExists), errors.Is(err, artifacts.ErrConflict):
		return http.StatusConflict, &EnvelopeError{Code: CodeConflict, Message: err.Error()}
	case errors.Is(err, artifacts.ErrInvalidInput),
		errors.Is(err, artifacts.ErrNameRequired),
		errors.Is(err, artifacts.ErrRefRequired),
		errors.Is(err, artifacts.ErrRefOrName),
		errors.Is(err, artifacts.ErrRefAndNameMutuallyExclusive),
		errors.Is(err, artifacts.ErrInvalidName),
		errors.Is(err, artifacts.ErrInvalidRef),
		errors.Is(err, artifacts.ErrUnsupportedURI):
		return http.StatusBadRequest, &EnvelopeError{Code: CodeInvalidInput, Message: err.Error()}
	default:
		return http.StatusInternalServerError, &EnvelopeError{Code: CodeInternal, Message: err.Error()}
	}
}
