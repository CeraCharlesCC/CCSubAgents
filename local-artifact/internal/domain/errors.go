package domain

import "errors"

var (
	ErrNotFound       = errors.New("not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrConflict       = errors.New("conflict")
	ErrInternal       = errors.New("internal")
	ErrNameRequired   = errors.New("name required")
	ErrRefOrName      = errors.New("either ref or name must be provided")
	ErrUnsupportedURI = errors.New("unsupported uri")
)
