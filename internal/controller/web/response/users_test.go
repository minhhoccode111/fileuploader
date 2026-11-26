package response

import (
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

func TestNewUserRow(t *testing.T) {
	t.Parallel()

	joined := time.Date(2026, time.October, 20, 23, 52, 0, 0, time.UTC)

	cases := []struct {
		name string
		role entity.UserRole
		want bool
	}{
		{name: "an admin", role: entity.UserRoleAdmin, want: true},
		{name: "a plain user", role: entity.UserRoleUser, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row := NewUserRow(&entity.UserListItem{
				Username:  "johndoe",
				Email:     "john@example.com",
				Role:      tc.role,
				CreatedAt: joined,
				UpdatedAt: joined.AddDate(0, 0, 1),
			}, UserDefaults{StorageQuotaBytes: 5368709120})

			if row.IsAdmin != tc.want {
				t.Errorf("IsAdmin = %v, want %v", row.IsAdmin, tc.want)
			}

			if row.Defaults.StorageQuotaBytes != 5368709120 {
				t.Errorf("Defaults.StorageQuotaBytes = %d, want the default passed in", row.Defaults.StorageQuotaBytes)
			}

			if row.Joined != "2026-10-20 23:52" {
				t.Errorf("Joined = %q, want %q", row.Joined, "2026-10-20 23:52")
			}

			if row.Updated != "2026-10-21 23:52" {
				t.Errorf("Updated = %q, want %q", row.Updated, "2026-10-21 23:52")
			}
		})
	}
}

// The storage cell words the bytes of the account against the quota that
// actually applies. A stored zero inherits the environment default, so the
// cell prints the default — and calls it Unlimited only when the default is
// unlimited too.
func TestUserRowStorageLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  UserRow
		want string
	}{
		{
			name: "an empty account with no defaults",
			row:  UserRow{},
			want: "0 B / Unlimited",
		},
		{
			name: "usage against an unlimited quota",
			row:  UserRow{StorageUsedBytes: 1536},
			want: "1.5 KB / Unlimited",
		},
		{
			name: "usage against a quota",
			row:  UserRow{StorageUsedBytes: 1536, StorageQuotaBytes: 1048576},
			want: "1.5 KB / 1.0 MB",
		},
		{
			name: "a stored zero inherits the default quota",
			row:  UserRow{StorageUsedBytes: 1536, Defaults: UserDefaults{StorageQuotaBytes: 5368709120}},
			want: "1.5 KB / 5.0 GB",
		},
		{
			name: "an override beats the default quota",
			row: UserRow{
				StorageUsedBytes:  1536,
				StorageQuotaBytes: 1048576,
				Defaults:          UserDefaults{StorageQuotaBytes: 5368709120},
			},
			want: "1.5 KB / 1.0 MB",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.row.StorageLabel(); got != tc.want {
				t.Errorf("StorageLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The drawer of the account editor words the limits an inheriting account
// actually runs at, so the note under an empty field names the default that
// applies instead of printing the stored zero.
func TestUserRowWordsInheritedLimits(t *testing.T) {
	t.Parallel()

	row := UserRow{Defaults: UserDefaults{
		StorageQuotaBytes:       5368709120,
		UploadRateBytesPerSec:   1048576,
		DownloadRateBytesPerSec: 0,
	}}

	if got := row.EffectiveStorageQuotaLabel(); got != "5.0 GB" {
		t.Errorf("EffectiveStorageQuotaLabel() = %q, want %q", got, "5.0 GB")
	}

	if got := row.EffectiveUploadRateLabel(); got != "1.0 MB/s" {
		t.Errorf("EffectiveUploadRateLabel() = %q, want %q", got, "1.0 MB/s")
	}

	if got := row.EffectiveDownloadRateLabel(); got != "Unlimited" {
		t.Errorf("EffectiveDownloadRateLabel() = %q, want %q", got, "Unlimited")
	}

	override := UserRow{
		UploadRateBytesPerSec: 2097152,
		Defaults:              row.Defaults,
	}

	if got := override.EffectiveUploadRateLabel(); got != "2.0 MB/s" {
		t.Errorf("EffectiveUploadRateLabel() = %q, want %q", got, "2.0 MB/s")
	}
}

// A click on the column the table is ordered by carries the filters and asks
// for the other direction. The header says which direction it is ordered by.
func TestUserFiltersSortActiveAscending(t *testing.T) {
	t.Parallel()

	filters := UserFilters{
		Query: "john",
		Role:  "admin",
		Date:  "2026-10-19",
		Limit: "5",
		Sort:  "username",
		Dir:   "asc",
	}

	sorted := filters.SortColumn("username")
	if !sorted.Active {
		t.Error("Active = false, want the column the table is ordered by")
	}

	if !sorted.Ascending {
		t.Error("Ascending = false, want the current direction")
	}

	if sorted.NextDirection != "desc" {
		t.Errorf("NextDirection = %q, want %q", sorted.NextDirection, "desc")
	}

	if got := sorted.AriaSort(); got != "ascending" {
		t.Errorf("AriaSort() = %q, want %q", got, "ascending")
	}

	wantQuery := "date=2026-10-19&dir=desc&limit=5&q=john&role=admin&sort=username"
	if sorted.Query != wantQuery {
		t.Errorf("Query = %q, want %q", sorted.Query, wantQuery)
	}

	if values := sorted.Values(); values["sort"] != "username" || values["dir"] != "desc" {
		t.Errorf("Values() = %v, want the column and the next direction", values)
	}
}

// The first click on another column starts it ascending and says that the
// table is not ordered by it.
func TestUserFiltersSortInactive(t *testing.T) {
	t.Parallel()

	filters := UserFilters{Sort: "username", Dir: "asc"}

	other := filters.SortColumn("email")
	if other.Active {
		t.Error("Active = true for a column the table is not ordered by")
	}

	if other.NextDirection != "asc" {
		t.Errorf("NextDirection = %q, want the first click to sort ascending", other.NextDirection)
	}

	if got := other.AriaSort(); got != "none" {
		t.Errorf("AriaSort() = %q, want %q", got, "none")
	}

	if !strings.Contains(other.Query, "sort=email") || !strings.Contains(other.Query, "dir=asc") {
		t.Errorf("Query = %q, want it to ask for the email column ascending", other.Query)
	}
}

// A descending order says so, and a click turns it ascending.
func TestUserFiltersSortDescending(t *testing.T) {
	t.Parallel()

	filters := UserFilters{Sort: "created_at", Dir: "desc"}

	descending := filters.SortColumn("created_at")
	if descending.Ascending {
		t.Error("Ascending = true for a descending order")
	}

	if descending.NextDirection != "asc" {
		t.Errorf("NextDirection = %q, want %q", descending.NextDirection, "asc")
	}

	if got := descending.AriaSort(); got != "descending" {
		t.Errorf("AriaSort() = %q, want %q", got, "descending")
	}
}

func TestNewUserPager(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		list   entity.UserList
		filter entity.UserFilter
		want   UserPager
	}{
		{
			name:   "an empty list",
			list:   entity.UserList{},
			filter: entity.UserFilter{Limit: 10, Page: 1},
			want:   UserPager{Page: 1, Pages: 1},
		},
		{
			name:   "a partial last page",
			list:   entity.UserList{Total: 12},
			filter: entity.UserFilter{Limit: 10, Page: 2},
			want:   UserPager{Page: 2, Pages: 2, Total: 12, Prev: 1},
		},
		{
			name:   "a middle page",
			list:   entity.UserList{Total: 30},
			filter: entity.UserFilter{Limit: 10, Page: 2},
			want:   UserPager{Page: 2, Pages: 3, Total: 30, Prev: 1, Next: 3},
		},
		{
			name:   "a page that fell off the end of the list",
			list:   entity.UserList{Total: 5},
			filter: entity.UserFilter{Limit: 10, Page: 4},
			want:   UserPager{Page: 1, Pages: 1, Total: 5},
		},
		{
			name:   "a page number below the first",
			list:   entity.UserList{Total: 5},
			filter: entity.UserFilter{Limit: 10, Page: 0},
			want:   UserPager{Page: 1, Pages: 1, Total: 5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pager := NewUserPager(tc.list, &tc.filter)

			if pager != tc.want {
				t.Errorf("pager = %+v, want %+v", pager, tc.want)
			}
		})
	}
}
