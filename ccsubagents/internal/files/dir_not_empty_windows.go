//go:build windows

package files

import (
	"errors"
	"syscall"
)

func IsDirNotEmptyError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ERROR_DIR_NOT_EMPTY)
}
