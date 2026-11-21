// Package file implements the Postgres-backed File repository.
package file

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
)

// _fileColumns is the column list every file read selects.
const _fileColumns = "id, user_id, folder_id, name, content_type, storage_key, size, created_at, updated_at"

// _userIDColumn is the column every query of the repository scopes by.
const _userIDColumn = "user_id"

// _deleteSubtreeFilesQuery deletes the rows of every file under a folder and
// returns the storage keys it removed, so the bytes can be cleaned up after
// the transaction commits. The user filter keeps the walk inside one account.
const _deleteSubtreeFilesQuery = `
WITH RECURSIVE tree AS (
    SELECT id FROM folders WHERE id = $2 AND user_id = $1
    UNION ALL
    SELECT child.id FROM folders child JOIN tree ON child.parent_id = tree.id
    WHERE child.user_id = $1
)
DELETE FROM files
WHERE user_id = $1 AND folder_id IN (SELECT id FROM tree)
RETURNING id, user_id, folder_id, name, content_type, storage_key, size, created_at, updated_at`

// Repo -.
type Repo struct {
	*postgres.Postgres
}

// New returns a File repository.
func New(pg *postgres.Postgres) repo.FileRepo {
	return &Repo{pg}
}

// Store -.
func (r *Repo) Store(ctx context.Context, file *entity.File) error {
	sql, args, err := r.Builder.
		Insert("files").
		Columns("id, user_id, folder_id, name, content_type, storage_key, size, created_at, updated_at").
		Values(
			file.ID,
			file.UserID,
			file.FolderID,
			file.Name,
			file.ContentType,
			file.StorageKey,
			file.Size,
			file.CreatedAt,
			file.UpdatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("FileRepo - Store - r.Builder: %w", err)
	}

	_, err = r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FileRepo - Store - r.Querier(ctx).Exec: %w", err)
	}

	return nil
}

// Get returns a file of the account. A file of another account looks missing.
func (r *Repo) Get(ctx context.Context, userID, fileID string) (entity.File, error) {
	sql, args, err := r.Builder.
		Select(_fileColumns).
		From("files").
		Where(sq.Eq{"id": fileID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return entity.File{}, fmt.Errorf("FileRepo - Get - r.Builder: %w", err)
	}

	return scanFile(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// List returns the files directly inside a folder, ordered by name.
func (r *Repo) List(ctx context.Context, userID, folderID string) ([]entity.FileItem, error) {
	sql, args, err := r.Builder.
		Select("id, name, content_type, size, updated_at").
		From("files").
		Where(sq.Eq{_userIDColumn: userID, "folder_id": folderID}).
		OrderBy("lower(name) ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("FileRepo - List - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("FileRepo - List - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	items := make([]entity.FileItem, 0)

	for rows.Next() {
		var item entity.FileItem

		err = rows.Scan(&item.ID, &item.Name, &item.ContentType, &item.Size, &item.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("FileRepo - List - rows.Scan: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FileRepo - List - rows.Err: %w", err)
	}

	return items, nil
}

// ListPage returns at most limit files directly inside a folder, in name
// order, resuming after a cursor. An empty folderID covers the whole own drive,
// which only global search uses, and an empty search matches every name. Path
// stays empty here: the caller fills it for a global search.
func (r *Repo) ListPage(
	ctx context.Context,
	userID, folderID, search string,
	after *entity.EntryCursor,
	limit int,
) ([]entity.FileItem, error) {
	sql, args, err := r.Builder.
		Select("id", "folder_id", "name", "content_type", "size", "updated_at").
		From("files").
		Where(filePageWhere(userID, folderID, search, after)).
		OrderBy("lower(name) ASC", "id ASC").
		Limit(uint64(max(limit, 1))).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("FileRepo - ListPage - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("FileRepo - ListPage - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	items := make([]entity.FileItem, 0)

	for rows.Next() {
		var item entity.FileItem

		err = rows.Scan(
			&item.ID,
			&item.FolderID,
			&item.Name,
			&item.ContentType,
			&item.Size,
			&item.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("FileRepo - ListPage - rows.Scan: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FileRepo - ListPage - rows.Err: %w", err)
	}

	return items, nil
}

// Rename stores the name a use case already resolved. A missing file maps to
// ErrFileNotFound.
func (r *Repo) Rename(ctx context.Context, file *entity.File) error {
	sql, args, err := r.Builder.
		Update("files").
		Set("name", file.Name).
		Set("updated_at", file.UpdatedAt).
		Where(sq.Eq{"id": file.ID, _userIDColumn: file.UserID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("FileRepo - Rename - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FileRepo - Rename - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrFileNotFound
	}

	return nil
}

// Move changes the folder a file is listed in. A missing file maps to
// ErrFileNotFound.
func (r *Repo) Move(ctx context.Context, userID, fileID, folderID string) error {
	sql, args, err := r.Builder.
		Update("files").
		Set("folder_id", folderID).
		Set("updated_at", time.Now().UTC()).
		Where(sq.Eq{"id": fileID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("FileRepo - Move - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FileRepo - Move - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrFileNotFound
	}

	return nil
}

// Delete removes a file of the account and returns the row it removed, so the
// caller still has the storage key to clean up.
func (r *Repo) Delete(ctx context.Context, userID, fileID string) (entity.File, error) {
	sql, args, err := r.Builder.
		Delete("files").
		Where(sq.Eq{"id": fileID, _userIDColumn: userID}).
		Suffix("RETURNING " + _fileColumns).
		ToSql()
	if err != nil {
		return entity.File{}, fmt.Errorf("FileRepo - Delete - r.Builder: %w", err)
	}

	return scanFile(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// NameTaken reports whether a file of the account already carries the name in
// the folder. The comparison ignores case, as the unique index does.
func (r *Repo) NameTaken(ctx context.Context, userID, folderID, name string) (bool, error) {
	sql, args, err := r.Builder.
		Select("count(*)").
		From("files").
		Where(sq.Eq{_userIDColumn: userID, "folder_id": folderID}).
		Where(sq.Expr("lower(name) = lower(?)", name)).
		ToSql()
	if err != nil {
		return false, fmt.Errorf("FileRepo - NameTaken - r.Builder: %w", err)
	}

	var count int

	err = r.Querier(ctx).QueryRow(ctx, sql, args...).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("FileRepo - NameTaken - r.Querier(ctx).QueryRow: %w", err)
	}

	return count > 0, nil
}

// DeleteByFolderSubtree removes the rows of every file under a folder and
// returns the storage keys it removed, so the bytes can be cleaned up after
// the transaction commits.
func (r *Repo) DeleteByFolderSubtree(
	ctx context.Context,
	userID, folderID string,
) ([]entity.File, error) {
	rows, err := r.Querier(ctx).Query(ctx, _deleteSubtreeFilesQuery, userID, folderID)
	if err != nil {
		return nil, fmt.Errorf("FileRepo - DeleteByFolderSubtree - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	files := make([]entity.File, 0)

	for rows.Next() {
		var file entity.File

		var folderID string

		err = rows.Scan(
			&file.ID,
			&file.UserID,
			&folderID,
			&file.Name,
			&file.ContentType,
			&file.StorageKey,
			&file.Size,
			&file.CreatedAt,
			&file.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("FileRepo - DeleteByFolderSubtree - rows.Scan: %w", err)
		}

		file.FolderID = folderID

		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FileRepo - DeleteByFolderSubtree - rows.Err: %w", err)
	}

	return files, nil
}

// filePageWhere builds the conditions a paged file read filters by, in a fixed
// order: the account, the folder when one is named, the search term when one is
// typed, and the cursor row when the page resumes.
func filePageWhere(
	userID, folderID, search string,
	after *entity.EntryCursor,
) sq.Sqlizer {
	conds := []sq.Sqlizer{sq.Eq{_userIDColumn: userID}}

	if folderID != "" {
		conds = append(conds, sq.Eq{"folder_id": folderID})
	}

	if search != "" {
		conds = append(conds, sq.Expr(
			"lower(name) LIKE lower(?) ESCAPE '\\'",
			entity.SearchPattern(search),
		))
	}

	if after != nil {
		conds = append(conds, sq.Expr(
			"(lower(name) > lower(?) OR (lower(name) = lower(?) AND id > ?::uuid))",
			after.Name,
			after.Name,
			after.ID,
		))
	}

	return sq.And(conds)
}

// scanFile reads one file row.
func scanFile(row pgx.Row) (entity.File, error) {
	var file entity.File

	err := row.Scan(
		&file.ID,
		&file.UserID,
		&file.FolderID,
		&file.Name,
		&file.ContentType,
		&file.StorageKey,
		&file.Size,
		&file.CreatedAt,
		&file.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.File{}, entity.ErrFileNotFound
		}

		return entity.File{}, fmt.Errorf("FileRepo - scanFile - row.Scan: %w", err)
	}

	return file, nil
}
