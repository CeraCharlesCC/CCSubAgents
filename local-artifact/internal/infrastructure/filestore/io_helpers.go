package filestore

import (
	"errors"
	"io"
	"os"
)

func closeIgnore(closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		_ = err
	}
}

func removeIfExists(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = err
	}
}
