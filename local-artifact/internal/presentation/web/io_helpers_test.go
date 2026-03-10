package web

import (
	"bytes"
	"net/http"
	"testing"
)

type shortResponseWriter struct {
	maxPerWrite int
	header      http.Header
	buf         bytes.Buffer
	calls       int
	status      int
}

func (w *shortResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *shortResponseWriter) Write(p []byte) (int, error) {
	w.calls++
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) > w.maxPerWrite {
		p = p[:w.maxPerWrite]
	}
	return w.buf.Write(p)
}

func (w *shortResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

type zeroResponseWriter struct {
	header http.Header
	calls  int
}

func (w *zeroResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *zeroResponseWriter) Write(_ []byte) (int, error) {
	w.calls++
	return 0, nil
}

func (w *zeroResponseWriter) WriteHeader(_ int) {}

func TestWritePayloadHandlesShortWrites(t *testing.T) {
	w := &shortResponseWriter{maxPerWrite: 2}

	writePayload(w, []byte("abcdef"))

	if got := w.buf.String(); got != "abcdef" {
		t.Fatalf("body = %q, want %q", got, "abcdef")
	}
	if w.calls < 3 {
		t.Fatalf("expected multiple writes, got %d", w.calls)
	}
}

func TestWritePayloadStopsOnZeroProgress(t *testing.T) {
	w := &zeroResponseWriter{}

	writePayload(w, []byte("abc"))

	if w.calls != 1 {
		t.Fatalf("calls = %d, want 1", w.calls)
	}
}
