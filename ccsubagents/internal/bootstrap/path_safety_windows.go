//go:build windows

package bootstrap

import (
	"errors"
	"syscall"
)

func isDirNotEmptyError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ERROR_DIR_NOT_EMPTY)
}
