package entity

import "testing"

// The account list always has an order. A filter that names an unknown column
// or an unknown direction falls back to the default of the list, which is the
// newest account first.
func TestUserFilterSanitizeSort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		filter   UserFilter
		wantSort UserSortField
		wantDir  UserSortDirection
	}{
		{
			name:     "an empty filter takes the default order",
			filter:   UserFilter{},
			wantSort: UserSortCreatedAt,
			wantDir:  UserSortDescending,
		},
		{
			name:     "a known column and direction stay",
			filter:   UserFilter{Sort: UserSortUsername, Dir: UserSortAscending},
			wantSort: UserSortUsername,
			wantDir:  UserSortAscending,
		},
		{
			name:     "an unknown column falls back to the default column",
			filter:   UserFilter{Sort: UserSortField("password_hash"), Dir: UserSortAscending},
			wantSort: UserSortCreatedAt,
			wantDir:  UserSortAscending,
		},
		{
			name:     "an unknown direction falls back to the default direction",
			filter:   UserFilter{Sort: UserSortEmail, Dir: UserSortDirection("sideways")},
			wantSort: UserSortEmail,
			wantDir:  UserSortDescending,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.filter.Sanitize()

			if tc.filter.Sort != tc.wantSort {
				t.Errorf("Sort = %q, want %q", tc.filter.Sort, tc.wantSort)
			}

			if tc.filter.Dir != tc.wantDir {
				t.Errorf("Dir = %q, want %q", tc.filter.Dir, tc.wantDir)
			}
		})
	}
}
