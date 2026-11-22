package entity

import "testing"

func FuzzDecodeEntryCursor(f *testing.F) {
	f.Add("")
	f.Add("not-base64")
	f.Add(EntryCursor{
		Kind: EntryKindFile,
		Name: "notes.txt",
		ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
	}.Encode())

	f.Fuzz(func(t *testing.T, encoded string) {
		cursor, err := DecodeEntryCursor(encoded)
		if err != nil {
			return
		}

		if cursor == nil {
			return
		}

		if cursor.Kind != EntryKindFolder && cursor.Kind != EntryKindFile {
			t.Fatalf("accepted kind %q", cursor.Kind)
		}

		if cursor.Name == "" || cursor.ID == "" {
			t.Fatalf("accepted empty field: %+v", cursor)
		}

		if decoded, err := DecodeEntryCursor(cursor.Encode()); err != nil || decoded == nil || *decoded != *cursor {
			t.Fatalf("round trip = %+v, %v; want %+v", decoded, err, cursor)
		}
	})
}
