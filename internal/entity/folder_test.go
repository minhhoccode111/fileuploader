package entity

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanNameKeepsValidNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "a plain name", in: "report.pdf", want: "report.pdf"},
		{name: "outer spaces", in: "  report.pdf  ", want: "report.pdf"},
		{name: "an inner space", in: "annual report", want: "annual report"},
		{name: "a unicode name", in: "báo cáo.txt", want: "báo cáo.txt"},
		{name: "the column width", in: strings.Repeat("a", 255), want: strings.Repeat("a", 255)},
		{name: "a name near a device name", in: "console.log", want: "console.log"},
		{name: "a device name with two digits", in: "COM10.txt", want: "COM10.txt"},
		{name: "a name with punctuation", in: "release #1 & notes.txt", want: "release #1 & notes.txt"},
		{name: "an emoji family", in: "family👨\u200d👩\u200d👧.txt", want: "family👨\u200d👩\u200d👧.txt"},
		{name: "a persian name with a non-joiner", in: "می\u200cخواهم.txt", want: "می\u200cخواهم.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := CleanName(tc.in)
			if err != nil {
				t.Fatalf("CleanName() error = %v, want nil", err)
			}

			if got != tc.want {
				t.Errorf("CleanName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCleanNameRejectsNamesTheDriveCannotStore(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{name: "an empty name", in: ""},
		{name: "only spaces", in: "   "},
		{name: "the dot entry", in: "."},
		{name: "the dot-dot entry", in: ".."},
		{name: "a forward slash", in: "a/b"},
		{name: "a backslash", in: `a\b`},
		{name: "a control character", in: "a\nb"},
		{name: "a nul byte", in: "a\x00b"},
		{name: "past the column width", in: strings.Repeat("a", 256)},
		{name: "a right-to-left override", in: "invoice\u202Etxt.exe"},
		{name: "a left-to-right isolate", in: "a\u2066b\u2069c"},
		{name: "a trailing dot", in: "report."},
		{name: "a colon", in: "a:b"},
		{name: "an angle bracket", in: "a<b"},
		{name: "a quote", in: `a"b`},
		{name: "a pipe", in: "a|b"},
		{name: "a question mark", in: "a?b"},
		{name: "an asterisk", in: "a*b"},
		{name: "the CON device", in: "CON"},
		{name: "the NUL device with an extension", in: "nul.txt"},
		{name: "the LPT9 device in another case", in: "Lpt9.log"},
		{name: "the COM3 device", in: "COM3.dat"},
		{name: "the CONOUT$ device", in: "CONOUT$.txt"},
		{name: "a zero-width space", in: "a\u200Bb"},
		{name: "a left-to-right mark", in: "a\u200Eb"},
		{name: "a word joiner", in: "a\u2060b"},
		{name: "an invisible plus", in: "a\u2064b"},
		{name: "a byte-order mark", in: "a\uFEFFb"},
		{name: "a line separator", in: "a\u2028b"},
		{name: "a paragraph separator", in: "a\u2029b"},
		{name: "invalid utf-8", in: string([]byte{0x61, 0xFF, 0x62})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := CleanName(tc.in)
			if !errors.Is(err, ErrInvalidName) {
				t.Errorf("CleanName() error = %v, want %v", err, ErrInvalidName)
			}
		})
	}
}
