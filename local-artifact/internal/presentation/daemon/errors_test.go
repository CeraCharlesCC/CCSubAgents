package daemon

import (
	"errors"
	"net/http"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestMapCoreError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid", err: artifacts.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidInput},
		{name: "not found", err: artifacts.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: CodeNotFound},
		{name: "conflict", err: artifacts.ErrConflict, wantStatus: http.StatusConflict, wantCode: CodeConflict},
		{name: "internal", err: errors.New("boom"), wantStatus: http.StatusInternalServerError, wantCode: CodeInternal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, payload := mapCoreError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status mismatch: got=%d want=%d", status, tc.wantStatus)
			}
			if payload == nil || payload.Code != tc.wantCode {
				t.Fatalf("code mismatch: payload=%+v want=%s", payload, tc.wantCode)
			}
		})
	}
}
