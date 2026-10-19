package entity

// Session is the account a live session belongs to. The role travels with the
// session, so a guard can check it without reading the database.
type Session struct {
	UserID string   `json:"user_id"`
	Role   UserRole `json:"role"`
}
