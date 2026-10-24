package entity

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// RootFolderName is the name the root folder of an account carries. It is
	// the only folder without a parent, and the only folder that may hold it.
	RootFolderName = "/"

	// _maxNameLength is the width of the name columns of the folders and files
	// tables. A name longer than that would be truncated by the database.
	_maxNameLength = 255
)

// Folder is one directory of an account's drive. The root folder is the only
// one with a nil parent, and Register creates it with the account.
type Folder struct {
	ID        string
	UserID    string
	ParentID  *string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsRoot reports whether the folder is the root of the account's drive. The
// root cannot be deleted or renamed, so every entry keeps a home.
func (f *Folder) IsRoot() bool {
	return f.ParentID == nil
}

// FolderRef is the name and identifier a breadcrumb renders for one ancestor
// of the folder on the screen.
type FolderRef struct {
	ID   string
	Name string
}

// FolderItem is one folder in a listing: its name, the number of entries
// directly inside it, and when it last changed.
type FolderItem struct {
	ID        string
	Name      string
	ItemCount int
	UpdatedAt time.Time
}

// FileItem is one file in a listing. The storage key stays in the database:
// a listing never needs it, and it is not the caller's business.
type FileItem struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	UpdatedAt   time.Time
}

// FolderView is everything the drive page renders for one folder: the folder
// itself, the path that leads to it, the folders and the files inside it.
type FolderView struct {
	Folder     Folder
	Breadcrumb []FolderRef
	Folders    []FolderItem
	Files      []FileItem
}

// CleanName normalizes a name a caller typed and rejects the ones the drive
// cannot store: an empty name, one longer than the column, a path separator,
// and a control character. The name is what the visitor sees, so it keeps the
// case and the spaces inside it.
func CleanName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > _maxNameLength {
		return "", ErrInvalidName
	}

	if name == "." || name == ".." {
		return "", ErrInvalidName
	}

	if strings.ContainsAny(name, `/\`) || strings.IndexByte(name, 0) >= 0 {
		return "", ErrInvalidName
	}

	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrInvalidName
	}

	return name, nil
}
