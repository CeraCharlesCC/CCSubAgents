package web

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
)

func closeIgnore(closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		_ = err
	}
}

func removeMultipartForm(form *multipart.Form) {
	if form == nil {
		return
	}
	if err := form.RemoveAll(); err != nil {
		_ = err
	}
}

func writePayload(w http.ResponseWriter, payload []byte) {
	if _, err := w.Write(payload); err != nil {
		_ = err
	}
}

func encodeJSON(w http.ResponseWriter, payload any) {
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		_ = err
	}
}

func shutdownHTTPServer(server *http.Server, ctx context.Context) {
	if server == nil {
		return
	}
	if err := server.Shutdown(ctx); err != nil {
		_ = err
	}
}
