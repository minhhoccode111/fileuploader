// Package repo implements application outer layer logic. Each logic group in own file.
package repo

import (
	"context"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

type (
	// UnitOfWork runs a group of repository calls inside a single database
	// transaction. The context it hands to the callback carries the
	// transaction, so every repository called with it writes inside the same
	// transaction. The transaction is committed when the callback returns nil
	// and rolled back when it returns an error.
	UnitOfWork interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	// UserRepo -.
	UserRepo interface {
		Store(ctx context.Context, user *entity.User) error
		GetByID(ctx context.Context, id string) (entity.User, error)
		GetByEmail(ctx context.Context, email string) (entity.User, error)
		List(ctx context.Context, filter *entity.UserFilter) (entity.UserList, error)
		UpdateRole(ctx context.Context, userID string, role entity.UserRole) error
		Update(ctx context.Context, user *entity.User) error
	}

	// SessionRepo -.
	SessionRepo interface {
		Store(ctx context.Context, id string, session entity.Session, ttl time.Duration) error
		Get(ctx context.Context, id string) (entity.Session, error)
		Delete(ctx context.Context, id string) error
		DeleteUserSessions(ctx context.Context, userID string) error
	}
)
