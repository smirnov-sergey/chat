package user

import "errors"

var (
	ErrNotFound           = errors.New("user not found")
	ErrPhotoNotFound      = errors.New("user photo not found")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid username or password")
)
