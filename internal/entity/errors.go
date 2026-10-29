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
	ErrFolderNotFound        = errors.New("folder not found")
	ErrFileNotFound          = errors.New("file not found")
	ErrEntryAlreadyExists    = errors.New("entry already exists")
	ErrInvalidName           = errors.New("invalid name")
	ErrFileTooLarge          = errors.New("file too large")
	ErrRootFolder            = errors.New("root folder cannot be changed")
	ErrFolderCycle           = errors.New("folder cannot be moved into itself or its subtree")
	ErrStorageCleanup        = errors.New("storage cleanup failed")
)
