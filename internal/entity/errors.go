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
	ErrInvalidLimit          = ErrInvalidLimits
	ErrInvalidUserLimits     = ErrInvalidLimits
	ErrInvalidUserLimit      = ErrInvalidLimits
	ErrInvalidQuota          = ErrInvalidLimits
	ErrInvalidStorageQuota   = ErrInvalidLimits
	ErrInvalidUploadRate     = ErrInvalidLimits
	ErrInvalidDownloadRate   = ErrInvalidLimits
	ErrInvalidRate           = ErrInvalidLimits
	ErrInvalidRates          = ErrInvalidLimits
	ErrNegativeLimit         = ErrInvalidLimits
	ErrNegativeLimits        = ErrInvalidLimits
	ErrNegativeValue         = ErrInvalidLimits
	ErrNegativeValues        = ErrInvalidLimits
	ErrNegativeQuota         = ErrInvalidLimits
	ErrNegativeRate          = ErrInvalidLimits
	ErrNegativeRates         = ErrInvalidLimits
	ErrInvalidValue          = ErrInvalidLimits
	ErrInvalidValues         = ErrInvalidLimits
	ErrFolderNotFound        = errors.New("folder not found")
	ErrFileNotFound          = errors.New("file not found")
	ErrEntryAlreadyExists    = errors.New("entry already exists")
	ErrInvalidName           = errors.New("invalid name")
	ErrFileTooLarge          = errors.New("file too large")
	ErrRootFolder            = errors.New("root folder cannot be changed")
	ErrFolderCycle           = errors.New("folder cannot be moved into itself or its subtree")
	ErrStorageCleanup        = errors.New("storage cleanup failed")
)
