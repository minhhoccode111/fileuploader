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

	cases := []string{
		"not-base64",
		strings.Repeat("a", 16*1024),
		badJSON,
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

func TestSearchPatternEscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()

	if got := SearchPattern(`100%_x\y`); got != `%100\%\_x\\y%` {
		t.Errorf("SearchPattern() = %q, want %q", got, `%100\%\_x\\y%`)
	}
}
