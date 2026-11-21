package web

import "testing"

// downloadDisposition carries the name twice: an ASCII fallback any client can
// read, and the exact UTF-8 name in filename* for the ones that follow RFC
// 5987. The fallback never contains a quote, a backslash or a control byte, so
// it cannot break out of the header.
func TestDownloadDispositionEscapesTheName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "an ascii name",
			in:   "note.txt",
			want: `attachment; filename="note.txt"; filename*=UTF-8''note.txt`,
		},
		{
			name: "a unicode name",
			in:   "naïve 日本.txt",
			want: `attachment; filename="na_ve __.txt"; filename*=UTF-8''na%C3%AFve%20%E6%97%A5%E6%9C%AC.txt`,
		},
		{
			name: "a quoted name",
			in:   `quote"name.txt`,
			want: `attachment; filename="quote_name.txt"; filename*=UTF-8''quote%22name.txt`,
		},
		{
			name: "a name with no ascii",
			in:   "日本",
			want: `attachment; filename="download"; filename*=UTF-8''%E6%97%A5%E6%9C%AC`,
		},
		{
			name: "a name with a backslash",
			in:   `a\b.txt`,
			want: `attachment; filename="a_b.txt"; filename*=UTF-8''a%5Cb.txt`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := downloadDisposition(tc.in); got != tc.want {
				t.Errorf("downloadDisposition(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
