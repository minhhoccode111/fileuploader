package response

import "github.com/minhhoccode111/fileuploader/internal/entity"

// _joinedLayout is the date the account list prints for an account's creation.
const _joinedLayout = "2006-01-02"

// UserRow is one account in the admin user table. It is a presentation type: the
// admin flag and the joined date are already what the template prints.
type UserRow struct {
	Username string
	Email    string
	IsAdmin  bool
	Joined   string
	Updated  string
}

// NewUserRow maps the read model to the row the account table renders.
func NewUserRow(item *entity.UserListItem) UserRow {
	return UserRow{
		Username: item.Username,
		Email:    item.Email,
		IsAdmin:  item.Role == entity.UserRoleAdmin,
		Joined:   item.CreatedAt.Format(_joinedLayout),
		Updated:  item.UpdatedAt.Format(_joinedLayout),
	}
}
