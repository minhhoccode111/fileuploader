package response

import (
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
			})

			if row.IsAdmin != tc.want {
				t.Errorf("IsAdmin = %v, want %v", row.IsAdmin, tc.want)
			}

			if row.Joined != "2026-10-20" {
				t.Errorf("Joined = %q, want %q", row.Joined, "2026-10-20")
			}

			if row.Updated != "2026-10-21" {
				t.Errorf("Updated = %q, want %q", row.Updated, "2026-10-21")
			}
		})
	}
}
