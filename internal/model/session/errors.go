package session

import "errors"

var (
	ErrNotFound = errors.New("session not found")
	ErrInvalid  = errors.New("invalid or expired session")
)
