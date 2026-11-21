package entity

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestEntryCursorRoundTrip(t *testing.T) {
	t.Parallel()

	in := EntryCursor{Kind: EntryKindFile, Name: "notes.txt", ID: "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44", FoldersDone: true}

	out, err := DecodeEntryCursor(in.Encode())
	if err != nil {
		t.Fatalf("DecodeEntryCursor() error = %v, want nil", err)
	}

	if out == nil || *out != in {
		t.Fatalf("DecodeEntryCursor() = %+v, want %+v", out, in)
	}
}

func TestDecodeEntryCursorRejectsBadValues(t *testing.T) {
	t.Parallel()

	badJSON := base64.RawURLEncoding.EncodeToString([]byte(`{"kind":"bogus","name":"a","id":"not-a-uuid"}`))
	nulName := base64.RawURLEncoding.EncodeToString([]byte(
		`{"kind":"folder","name":"do\u0000cs","id":"d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"}`,
	))

	cases := []string{
		"not-base64",
		strings.Repeat("a", 16*1024),
		badJSON,
		nulName,
	}
	for _, encoded := range cases {
		if _, err := DecodeEntryCursor(encoded); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeEntryCursor(%q) error = %v, want %v", encoded, err, ErrInvalidCursor)
		}
	}

	if got, err := DecodeEntryCursor(""); err != nil || got != nil {
		t.Errorf("DecodeEntryCursor(\"\") = (%+v, %v), want (nil, nil)", got, err)
	}
}

func TestCleanSearchTrimsCapsAndDropsInvalidUTF8(t *testing.T) {
	t.Parallel()

	if got := CleanSearch("  notes  "); got != "notes" {
		t.Errorf("CleanSearch() = %q, want %q", got, "notes")
	}

	if got := CleanSearch(strings.Repeat("a", 101)); len([]rune(got)) != 100 {
		t.Errorf("CleanSearch() length = %d, want 100", len([]rune(got)))
	}

	if got := CleanSearch("a" + string([]byte{0xff}) + "b"); got != "ab" {
		t.Errorf("CleanSearch() = %q, want %q", got, "ab")
	}
}

// A stored name cannot hold a control character, so a term that carries one
// can never match: it must be dropped instead of reaching Postgres, which
// rejects the parameter and turns the search into a server fault.
func TestCleanSearchDropsControlCharacters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "\x00abc", want: "abc"},
		{in: "a\x00b", want: "ab"},
		{in: "a\nb\tc", want: "abc"},
		{in: "  \x00notes\x00  ", want: "notes"},
	}

	for _, tc := range cases {
		if got := CleanSearch(tc.in); got != tc.want {
			t.Errorf("CleanSearch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A cursor can be hand-built, so every field the listing resumes from has to
// name a row the drive can store. A kind the repositories do not know, a name
// no folder can carry, and an identifier that is not a UUID all fail here.
func TestEntryCursorValidateRejectsUnusableValues(t *testing.T) {
	t.Parallel()

	const validID = "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"

	cases := []struct {
		name   string
		cursor EntryCursor
	}{
		{name: "unknown kind", cursor: EntryCursor{Kind: "drive", Name: "docs", ID: validID}},
		{name: "bad id", cursor: EntryCursor{Kind: EntryKindFolder, Name: "docs", ID: "not-a-uuid"}},
		{name: "empty id", cursor: EntryCursor{Kind: EntryKindFolder, Name: "docs"}},
		{name: "empty name", cursor: EntryCursor{Kind: EntryKindFolder, ID: validID}},
		{
			name:   "256-rune name",
			cursor: EntryCursor{Kind: EntryKindFolder, Name: strings.Repeat("a", 256), ID: validID},
		},
		{name: "NUL in name", cursor: EntryCursor{Kind: EntryKindFolder, Name: "do\x00cs", ID: validID}},
		{name: "control in name", cursor: EntryCursor{Kind: EntryKindFile, Name: "do\ncs", ID: validID}},
		{
			name:   "invalid UTF-8 name",
			cursor: EntryCursor{Kind: EntryKindFile, Name: string([]byte{0xff}), ID: validID},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.cursor.validate(); !errors.Is(err, ErrInvalidCursor) {
				t.Errorf("validate() error = %v, want %v", err, ErrInvalidCursor)
			}
		})
	}

	valid := EntryCursor{Kind: EntryKindFolder, Name: "docs", ID: validID}
	if err := valid.validate(); err != nil {
		t.Errorf("validate() error = %v, want nil", err)
	}
}

func TestSearchPatternEscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()

	if got := SearchPattern(`100%_x\y`); got != `%100\%\_x\\y%` {
		t.Errorf("SearchPattern() = %q, want %q", got, `%100\%\_x\\y%`)
	}
}
