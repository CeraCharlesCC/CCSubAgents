package daemonapi

type Envelope struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *EnvelopeError `json:"error,omitempty"`
}

const (
	CodeInvalidInput       = "INVALID_INPUT"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeInternal           = "INTERNAL"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
