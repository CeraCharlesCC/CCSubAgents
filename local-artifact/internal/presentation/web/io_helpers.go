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
	for len(payload) > 0 {
		n, err := w.Write(payload)
		if err != nil {
			_ = err
			return
		}
		if n <= 0 {
			return
		}
		payload = payload[n:]
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
