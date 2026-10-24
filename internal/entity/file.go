package entity

import "time"

// File is one object of an account's drive. The bytes live in the object
// storage under StorageKey, which is built from the identifiers and not from
// the name, so a rename never touches the stored object.
type File struct {
	ID          string
	UserID      string
	FolderID    string
	Name        string
	ContentType string
	StorageKey  string
	Size        int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
