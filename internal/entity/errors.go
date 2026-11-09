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
	ErrInvalidLimits         = errors.New("invalid limits")
	ErrFolderNotFound        = errors.New("folder not found")
	ErrFileNotFound          = errors.New("file not found")
	ErrEntryAlreadyExists    = errors.New("entry already exists")
	ErrInvalidName           = errors.New("invalid name")
	ErrFileTooLarge          = errors.New("file too large")
	ErrStorageQuotaExceeded  = errors.New("storage quota exceeded")
	ErrRootFolder            = errors.New("root folder cannot be changed")
	ErrFolderCycle           = errors.New("folder cannot be moved into itself or its subtree")
	ErrFolderTooDeep         = errors.New("folder nesting is too deep")
	ErrStorageCleanup        = errors.New("storage cleanup failed")
	ErrShareNotFound         = errors.New("share not found")
	ErrShareExpired          = errors.New("share expired")
)
