package daemonclient

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
}

type RemoteError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *RemoteError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}
