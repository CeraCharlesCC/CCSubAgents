package domain

import "errors"

var (
	ErrNotFound                    = errors.New("not found")
	ErrInvalidInput                = errors.New("invalid input")
	ErrConflict                    = errors.New("conflict")
	ErrInternal                    = errors.New("internal")
	ErrNameRequired                = errors.New("name required")
	ErrRefRequired                 = errors.New("ref required")
	ErrRefOrName                   = errors.New("either ref or name must be provided")
	ErrRefAndNameMutuallyExclusive = errors.New("ref and name cannot both be provided")
	ErrUnsupportedURI              = errors.New("unsupported uri")
	ErrInvalidName                 = errors.New("invalid name")
	ErrInvalidRef                  = errors.New("invalid ref")
	ErrAliasExists                 = errors.New("alias already exists")
)
