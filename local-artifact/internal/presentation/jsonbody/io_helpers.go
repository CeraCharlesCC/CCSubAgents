package jsonbody

import "io"

func closeIgnore(closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		_ = err
	}
}
