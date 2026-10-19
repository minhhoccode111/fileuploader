package entity

import "errors"

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUserAlreadyExists     = errors.New("user already exists")
	ErrEmailAlreadyExists    = errors.New("email already exists")
	ErrUsernameAlreadyExists = errors.New("username already exists")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrPasswordTooLong       = errors.New("password too long")
	ErrSessionNotFound       = errors.New("session not found")
	ErrInvalidRole           = errors.New("invalid role")
)
