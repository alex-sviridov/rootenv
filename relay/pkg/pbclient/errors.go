package pbclient

import "errors"

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
)
