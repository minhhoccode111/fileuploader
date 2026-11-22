// Package share implements the Postgres-backed Share repository.
package share

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
)

const (
	_shareColumns   = "id, user_id, folder_id, expires_at, created_at, updated_at"
	_userIDColumn   = "user_id"
	_folderIDColumn = "folder_id"
)

// Repo -.
type Repo struct {
	*postgres.Postgres
}

// New returns a Share repository.
func New(pg *postgres.Postgres) repo.ShareRepo {
	return &Repo{pg}
}

// Store -.
func (r *Repo) Store(ctx context.Context, share *entity.Share) error {
	sql, args, err := r.Builder.
		Insert("shares").
		Columns("id, user_id, folder_id, expires_at, created_at, updated_at").
		Values(
			share.ID,
			share.UserID,
			share.FolderID,
			share.ExpiresAt,
			share.CreatedAt,
			share.UpdatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("ShareRepo - Store - r.Builder: %w", err)
	}

	_, err = r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("ShareRepo - Store - r.Querier(ctx).Exec: %w", err)
	}

	return nil
}

// Get returns a share by its token. A missing row is a share that does not
// exist or has been removed.
func (r *Repo) Get(ctx context.Context, shareID string) (entity.Share, error) {
	sql, args, err := r.Builder.
		Select(_shareColumns).
		From("shares").
		Where(sq.Eq{"id": shareID}).
		ToSql()
	if err != nil {
		return entity.Share{}, fmt.Errorf("ShareRepo - Get - r.Builder: %w", err)
	}

	return scanShare(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// activeShareWhere keeps the expiry condition in one parenthesized expression.
// Without the parentheses, AND binds first and unexpired shares from other
// accounts satisfy the unparenthesized OR.
func activeShareWhere() sq.Sqlizer {
	return sq.Expr("(expires_at IS NULL OR expires_at > now())")
}

// GetActiveByUserAndFolder returns the active share for a folder, if one
// exists. An expired share is not active, so the caller creates a new one.
func (r *Repo) GetActiveByUserAndFolder(ctx context.Context, userID, folderID string) (entity.Share, error) {
	sql, args, err := r.Builder.
		Select(_shareColumns).
		From("shares").
		Where(sq.Eq{_userIDColumn: userID, _folderIDColumn: folderID}).
		Where(activeShareWhere()).
		OrderBy("created_at DESC").
		Limit(1).
		ToSql()
	if err != nil {
		return entity.Share{}, fmt.Errorf("ShareRepo - GetActiveByUserAndFolder - r.Builder: %w", err)
	}

	return scanShare(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// GetByUserAndFolder returns any share for a folder, active or expired, so an
// expired link can be revived instead of leaving a dead row behind.
func (r *Repo) GetByUserAndFolder(ctx context.Context, userID, folderID string) (entity.Share, error) {
	sql, args, err := r.Builder.
		Select(_shareColumns).
		From("shares").
		Where(sq.Eq{_userIDColumn: userID, _folderIDColumn: folderID}).
		OrderBy("created_at DESC").
		Limit(1).
		ToSql()
	if err != nil {
		return entity.Share{}, fmt.Errorf("ShareRepo - GetByUserAndFolder - r.Builder: %w", err)
	}

	return scanShare(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// ListByUser returns the active shares of an account, newest first.
func (r *Repo) ListByUser(ctx context.Context, userID string) ([]entity.Share, error) {
	sql, args, err := r.Builder.
		Select(_shareColumns).
		From("shares").
		Where(sq.Eq{_userIDColumn: userID}).
		Where(activeShareWhere()).
		OrderBy("created_at DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("ShareRepo - ListByUser - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("ShareRepo - ListByUser - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	shares := make([]entity.Share, 0)

	for rows.Next() {
		var share entity.Share

		err = rows.Scan(
			&share.ID,
			&share.UserID,
			&share.FolderID,
			&share.ExpiresAt,
			&share.CreatedAt,
			&share.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("ShareRepo - ListByUser - rows.Scan: %w", err)
		}

		shares = append(shares, share)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ShareRepo - ListByUser - rows.Err: %w", err)
	}

	return shares, nil
}

// UpdateExpiry changes the expiry of a share owned by the account.
func (r *Repo) UpdateExpiry(ctx context.Context, userID, shareID string, expiresAt *time.Time) error {
	sql, args, err := r.Builder.
		Update("shares").
		Set("expires_at", expiresAt).
		Set("updated_at", time.Now().UTC()).
		Where(sq.Eq{"id": shareID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("ShareRepo - UpdateExpiry - r.Builder: %w", err)
	}

	result, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("ShareRepo - UpdateExpiry - r.Querier(ctx).Exec: %w", err)
	}

	if result.RowsAffected() == 0 {
		return entity.ErrShareNotFound
	}

	return nil
}

// Delete removes a share owned by the account.
func (r *Repo) Delete(ctx context.Context, userID, shareID string) error {
	sql, args, err := r.Builder.
		Delete("shares").
		Where(sq.Eq{"id": shareID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("ShareRepo - Delete - r.Builder: %w", err)
	}

	result, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("ShareRepo - Delete - r.Querier(ctx).Exec: %w", err)
	}

	if result.RowsAffected() == 0 {
		return entity.ErrShareNotFound
	}

	return nil
}

func scanShare(row pgx.Row) (entity.Share, error) {
	var share entity.Share

	err := row.Scan(
		&share.ID,
		&share.UserID,
		&share.FolderID,
		&share.ExpiresAt,
		&share.CreatedAt,
		&share.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Share{}, entity.ErrShareNotFound
		}

		return entity.Share{}, fmt.Errorf("ShareRepo - scanShare - row.Scan: %w", err)
	}

	return share, nil
}
