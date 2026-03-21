package main

import (
	"bytes"
	"io"
	"testing"
)

type shortWriter struct {
	maxPerWrite int
	buf         bytes.Buffer
	calls       int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.calls++
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) > w.maxPerWrite {
		p = p[:w.maxPerWrite]
	}
	return w.buf.Write(p)
}

type zeroWriter struct {
	calls int
}

func (w *zeroWriter) Write(_ []byte) (int, error) {
	w.calls++
	return 0, nil
}

func TestWriteAllHandlesShortWrites(t *testing.T) {
	w := &shortWriter{maxPerWrite: 3}

	if err := writeAll(w, []byte("abcdefg")); err != nil {
		t.Fatalf("writeAll returned error: %v", err)
	}
	if got := w.buf.String(); got != "abcdefg" {
		t.Fatalf("buffer = %q, want %q", got, "abcdefg")
	}
	if w.calls < 3 {
		t.Fatalf("expected multiple writes, got %d", w.calls)
	}
}

func TestWriteAllRejectsZeroProgress(t *testing.T) {
	w := &zeroWriter{}

	err := writeAll(w, []byte("abc"))
	if err == nil {
		t.Fatal("expected error")
	}
	if err != io.ErrShortWrite {
		t.Fatalf("error = %v, want %v", err, io.ErrShortWrite)
	}
	if w.calls != 1 {
		t.Fatalf("calls = %d, want 1", w.calls)
	}
}
