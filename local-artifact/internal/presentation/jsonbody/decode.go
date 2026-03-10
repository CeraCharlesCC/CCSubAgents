package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

// DecodeStrictJSON enforces a strict max body size, unknown field rejection, and a single JSON value.
func DecodeStrictJSON(r *http.Request, maxBytes int64, out any) error {
	if r == nil || r.Body == nil || maxBytes <= 0 {
		return fmt.Errorf("%w: invalid JSON body", artifacts.ErrInvalidInput)
	}
	defer closeIgnore(r.Body)

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("%w: invalid JSON body", artifacts.ErrInvalidInput)
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("%w: invalid JSON body", artifacts.ErrInvalidInput)
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: invalid JSON body", artifacts.ErrInvalidInput)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: invalid JSON body", artifacts.ErrInvalidInput)
	}
	return nil
}
