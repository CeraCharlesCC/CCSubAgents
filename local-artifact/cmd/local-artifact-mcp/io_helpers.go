package main

import (
	"fmt"
	"io"
)

func writeln(w io.Writer, args ...any) {
	if _, err := fmt.Fprintln(w, args...); err != nil {
		_ = err
	}
}
