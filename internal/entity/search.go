package entity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// _maxCursorBytes bounds the encoded cursor a request may carry. A page
	// link holds a few hundred bytes, so anything larger is not a cursor the
	// server issued.
	_maxCursorBytes = 8192

	// _maxSearchRunes bounds the term a search runs with. A longer term would
	// not narrow the drive any further and only costs work.
	_maxSearchRunes = 100
)

// EntryKind tells which table a listing row came from, so a cursor can resume
// the folder pass before the file pass.
type EntryKind string

const (
	// EntryKindFolder marks a cursor that points at a folder row.
	EntryKindFolder EntryKind = "folder"

	// EntryKindFile marks a cursor that points at a file row.
	EntryKindFile EntryKind = "file"
)

// EntryCursor is the last entry of a listing page. The next page asks the
// matching repository for the rows after it, ordered by lower(name) and then
// by id. FoldersDone records that the folder pass was already complete when
// the cursor points at a file.
type EntryCursor struct {
	Kind        EntryKind `json:"kind"`
	Name        string    `json:"name"`
	ID          string    `json:"id"`
	FoldersDone bool      `json:"foldersDone,omitempty"`
}

// Encode renders the cursor as the opaque value a page link carries: the JSON
// of the cursor in unpadded base64, so the value needs no URL escaping.
func (c EntryCursor) Encode() string {
	raw, err := json.Marshal(c)
	if err != nil {
		// The cursor holds a string, two strings and a bool, so marshaling it
		// cannot fail. An empty value would ask for the first page, which is
		// the safe answer if that ever changes.
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeEntryCursor reads the value Encode produced. An empty value asks for
// the first page, so it decodes to (nil, nil). Every other failure reports
// ErrInvalidCursor, which a caller answers with a client error instead of
// guessing at a page.
func DecodeEntryCursor(encoded string) (*EntryCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	if len(encoded) > _maxCursorBytes {
		return nil, ErrInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("entity - DecodeEntryCursor - base64.DecodeString: %w", ErrInvalidCursor)
	}

	var cursor EntryCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return nil, fmt.Errorf("entity - DecodeEntryCursor - json.Unmarshal: %w", ErrInvalidCursor)
	}

	if err := cursor.validate(); err != nil {
		return nil, fmt.Errorf("entity - DecodeEntryCursor - cursor.validate: %w", err)
	}

	return &cursor, nil
}

// validate reports whether the cursor describes a row a listing can resume
// from. A caller can hand-build the value, so a kind the repositories do not
// know, a name the drive cannot store, and an identifier that is not a UUID
// all have to fail here.
func (c EntryCursor) validate() error {
	if c.Kind != EntryKindFolder && c.Kind != EntryKindFile {
		return ErrInvalidCursor
	}

	if c.Name == "" || utf8.RuneCountInString(c.Name) > _maxNameLength {
		return ErrInvalidCursor
	}

	if _, err := uuid.Parse(c.ID); err != nil {
		return ErrInvalidCursor
	}

	return nil
}

// CleanSearch normalizes the term a search runs with: it drops the invalid
// UTF-8 a form may carry, trims the surrounding space, and keeps at most 100
// runes. An empty result means the visitor searched for nothing, which lists
// the whole folder.
func CleanSearch(raw string) string {
	search := strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if utf8.RuneCountInString(search) <= _maxSearchRunes {
		return search
	}

	return string([]rune(search)[:_maxSearchRunes])
}

// SearchPattern turns a search term into the LIKE pattern the repositories
// bind next to lower(name). It escapes the escape character and the two
// wildcards, so the term matches as a substring instead of a pattern, and
// wraps the result in the wildcards that allow a substring anywhere in the
// name. The query lowercases both sides to make the match case-insensitive.
func SearchPattern(term string) string {
	escaped := strings.NewReplacer(
		`\`, `\\`,
		"%", `\%`,
		"_", `\_`,
	).Replace(term)

	return "%" + escaped + "%"
}
