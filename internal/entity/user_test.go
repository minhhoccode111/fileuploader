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

func TestEffectiveLimitsInheritDefaults(t *testing.T) {
	t.Parallel()

	user := User{}
	if got := user.EffectiveStorageQuota(100); got != 100 {
		t.Errorf("quota = %d, want 100", got)
	}

	if got := user.EffectiveUploadRate(10); got != 10 {
		t.Errorf("upload rate = %d, want 10", got)
	}

	if got := user.EffectiveDownloadRate(20); got != 20 {
		t.Errorf("download rate = %d, want 20", got)
	}
}

func TestEffectiveLimitsUseOverride(t *testing.T) {
	t.Parallel()

	user := User{
		StorageQuotaBytes:       50,
		UploadRateBytesPerSec:   7,
		DownloadRateBytesPerSec: 9,
	}
	if got := user.EffectiveStorageQuota(100); got != 50 {
		t.Errorf("quota = %d, want 50", got)
	}

	if got := user.EffectiveUploadRate(10); got != 7 {
		t.Errorf("upload rate = %d, want 7", got)
	}

	if got := user.EffectiveDownloadRate(20); got != 9 {
		t.Errorf("download rate = %d, want 9", got)
	}
}
