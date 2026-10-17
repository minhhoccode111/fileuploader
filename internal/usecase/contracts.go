// Package usecase implements application business logic. Each logic group in own file.
package usecase

import (
	"context"

	"github.com/evrone/go-clean-template/internal/entity"
)

type (
	// User -.
	User interface {
		Register(ctx context.Context, username, email, password string) (entity.User, error)
		Login(ctx context.Context, email, password string) (string, error)
		Authenticate(ctx context.Context, sessionID string) (string, error)
		Logout(ctx context.Context, sessionID string) error
		GetUser(ctx context.Context, userID string) (entity.User, error)
	}
)
