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

	// MaxFolderDepth is the deepest level of nesting a drive may hold. The
	// root folder sits at depth 0, so a folder at this depth is the deepest
	// one that may be created.
	MaxFolderDepth = 20

	// _maxNameLength is the width of the name columns of the folders and files
	// tables. A name longer than that would be truncated by the database.
	_maxNameLength = 255
)

// FolderDepth reports how many ancestors the folder with the given identifier
// has among the list. A root folder has depth 0.
func FolderDepth(folders []Folder, id string) int {
	parentOf := make(map[string]*string, len(folders))
	for i := range folders {
		f := folders[i]
		parentOf[f.ID] = f.ParentID
	}

	depth := 0
	current := id
	seen := make(map[string]bool)

	for {
		parent, ok := parentOf[current]
		if !ok || parent == nil {
			break
		}

		if seen[current] {
			break
		}

		seen[current] = true
		current = *parent
		depth++
	}

	return depth
}

// FolderSubtreeHeight reports the deepest level of nesting below the folder
// with the given identifier, relative to it. A leaf folder has height 0.
func FolderSubtreeHeight(folders []Folder, id string) int {
	children := make(map[string][]string, len(folders))
	for i := range folders {
		f := folders[i]
		if f.ParentID != nil {
			children[*f.ParentID] = append(children[*f.ParentID], f.ID)
		}
	}

	maxHeight := 0

	var dfs func(node string, height int)

	dfs = func(node string, height int) {
		if height > maxHeight {
			maxHeight = height
		}

		for _, child := range children[node] {
			dfs(child, height+1)
		}
	}

	dfs(id, 0)

	return maxHeight
}

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
// cannot store: an empty name, one longer than the column, one that is not
// valid UTF-8, a path separator, a character Windows forbids, a control or
// invisible format character, a trailing dot, and a name Windows would open as
// a device. The name is what the visitor sees, so it keeps the case and the
// spaces inside it.
func CleanName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalidName
	}

	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > _maxNameLength {
		return "", ErrInvalidName
	}

	if name == "." || name == ".." {
		return "", ErrInvalidName
	}

	if forbiddenNameRune(name) || strings.HasSuffix(name, ".") || windowsDeviceName(name) {
		return "", ErrInvalidName
	}

	return name, nil
}

// forbiddenNameRune reports whether the name holds a path separator, a
// character Windows forbids, or a control or invisible format character.
func forbiddenNameRune(name string) bool {
	if strings.ContainsAny(name, `/\<>:"|?*`) || strings.IndexByte(name, 0) >= 0 {
		return true
	}

	return strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || isInvisibleFormat(r)
	})
}

// isInvisibleFormat reports whether r is a format character that reorders,
// hides or joins the text around it without showing up as a character of its
// own. A displayed name could then read differently from the one that is
// stored. The zero-width joiner and non-joiner stay allowed: emoji sequences
// and several living scripts need them.
func isInvisibleFormat(r rune) bool {
	switch {
	case r >= '\u202A' && r <= '\u202E':
		return true
	case r >= '\u2066' && r <= '\u2069':
		return true
	case r == '\u200B', r == '\u200E', r == '\u200F':
		return true
	case r >= '\u2060' && r <= '\u2064':
		return true
	case r == '\u2028', r == '\u2029', r == '\uFEFF':
		return true
	}

	return false
}

// windowsDeviceName reports whether the name would open a Windows device
// instead of a file. The rule reads the part before the first dot, so
// "NUL.txt" is a device too, and it compares the names in any case.
func windowsDeviceName(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	base = strings.ToUpper(base)

	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}

	if len(base) != 4 || (!strings.HasPrefix(base, "COM") && !strings.HasPrefix(base, "LPT")) {
		return false
	}

	return base[3] >= '1' && base[3] <= '9'
}
