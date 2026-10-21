// Package usecase implements application business logic. Each logic group in own file.
package usecase

import (
	"context"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

type (
	// User -.
	User interface {
		Register(ctx context.Context, username, email, password string) (entity.User, error)
		Login(ctx context.Context, email, password string) (string, entity.User, error)
		Authenticate(ctx context.Context, sessionID string) (entity.Session, error)
		Logout(ctx context.Context, sessionID string) error
		GetUser(ctx context.Context, userID string) (entity.User, error)
		ListUsers(ctx context.Context) ([]entity.UserListItem, error)
		SetRole(ctx context.Context, email string, role entity.UserRole) error
	}
)
