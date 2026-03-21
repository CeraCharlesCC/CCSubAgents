package daemon

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
)

func writef(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		_ = err
	}
}

func writeln(w io.Writer, args ...any) {
	if _, err := fmt.Fprintln(w, args...); err != nil {
		_ = err
	}
}

func closeIgnore(closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		_ = err
	}
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		_ = err
	}
}

func closeListenerIgnore(ln net.Listener) {
	if ln == nil {
		return
	}
	if err := ln.Close(); err != nil {
		_ = err
	}
}

func closeConnIgnore(conn net.Conn) {
	if conn == nil {
		return
	}
	if err := conn.Close(); err != nil {
		_ = err
	}
}

func removeIfExists(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = err
	}
}
