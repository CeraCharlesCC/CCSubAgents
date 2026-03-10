package main

import (
	"fmt"
	"io"
)

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func writeln(w io.Writer, args ...any) error {
	_, err := fmt.Fprintln(w, args...)
	return err
}

func writeAll(w io.Writer, b []byte) error {
	_, err := w.Write(b)
	return err
}
