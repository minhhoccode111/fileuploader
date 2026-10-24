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
