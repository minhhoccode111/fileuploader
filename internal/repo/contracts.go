// Package repo implements application outer layer logic. Each logic group in own file.
package repo

import (
	"context"
	"time"

	"github.com/evrone/go-clean-template/internal/entity"
)

type (
	// UserRepo -.
	UserRepo interface {
		Store(ctx context.Context, user *entity.User) error
		GetByID(ctx context.Context, id string) (entity.User, error)
		GetByEmail(ctx context.Context, email string) (entity.User, error)
	}

	// SessionRepo -.
	SessionRepo interface {
		Store(ctx context.Context, id, userID string, ttl time.Duration) error
		GetUserID(ctx context.Context, id string) (string, error)
		Delete(ctx context.Context, id string) error
	}
)
