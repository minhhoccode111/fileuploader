// Package user implements the Postgres-backed User repository.
package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
)

// Repo -.
type Repo struct {
	*postgres.Postgres
}

// New returns a User repository.
func New(pg *postgres.Postgres) repo.UserRepo {
	return &Repo{pg}
}

// Store -.
func (r *Repo) Store(ctx context.Context, user *entity.User) error {
	sql, args, err := r.Builder.
		Insert("users").
		Columns("id, username, email, password_hash, role, created_at, updated_at").
		Values(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("UserRepo - Store - r.Builder: %w", err)
	}

	_, err = r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uniqueViolation(pgErr)
		}

		return fmt.Errorf("UserRepo - Store - r.Querier(ctx).Exec: %w", err)
	}

	return nil
}

// uniqueViolation maps a Postgres unique-constraint violation to the sentinel for
// the column that clashed, so the transport can name the offending field.
func uniqueViolation(pgErr *pgconn.PgError) error {
	switch {
	case strings.Contains(pgErr.ConstraintName, "email"):
		return entity.ErrEmailAlreadyExists
	case strings.Contains(pgErr.ConstraintName, "username"):
		return entity.ErrUsernameAlreadyExists
	default:
		return entity.ErrUserAlreadyExists
	}
}

// GetByID -.
func (r *Repo) GetByID(ctx context.Context, id string) (entity.User, error) {
	return r.getUser(ctx, "id", id)
}

// GetByEmail -.
func (r *Repo) GetByEmail(ctx context.Context, email string) (entity.User, error) {
	return r.getUser(ctx, "email", email)
}

// UpdateRole changes the role of an account. A missing account maps to
// ErrUserNotFound.
func (r *Repo) UpdateRole(ctx context.Context, userID string, role entity.UserRole) error {
	sql, args, err := r.Builder.
		Update("users").
		Set("role", role).
		Set("updated_at", time.Now().UTC()).
		Where(sq.Eq{"id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("UserRepo - UpdateRole - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("UserRepo - UpdateRole - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrUserNotFound
	}

	return nil
}

func (r *Repo) getUser(ctx context.Context, column, value string) (entity.User, error) {
	sql, args, err := r.Builder.
		Select("id, username, email, password_hash, role, created_at, updated_at").
		From("users").
		Where(sq.Eq{column: value}).
		ToSql()
	if err != nil {
		return entity.User{}, fmt.Errorf("UserRepo - getUser - r.Builder: %w", err)
	}

	var user entity.User

	err = r.Querier(ctx).QueryRow(ctx, sql, args...).
		Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.User{}, entity.ErrUserNotFound
		}

		return entity.User{}, fmt.Errorf("UserRepo - getUser - r.Querier(ctx).QueryRow: %w", err)
	}

	return user, nil
}
