// Package folder implements the Postgres-backed Folder repository.
package folder

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

// _folderColumns is the column list every folder read selects, with the
// nullable parent flattened to an empty string so the scan stays simple.
const _folderColumns = "id, user_id, coalesce(parent_id::text, ''), name, created_at, updated_at"

// _breadcrumbQuery walks from a folder up to the root with a recursive CTE and
// returns the path root first.
const _breadcrumbQuery = `
WITH RECURSIVE ancestors AS (
    SELECT id, parent_id, name, 0 AS depth
    FROM folders
    WHERE id = $2 AND user_id = $1
    UNION ALL
    SELECT parent.id, parent.parent_id, parent.name, child.depth + 1
    FROM folders parent
    JOIN ancestors child ON parent.id = child.parent_id
    WHERE parent.user_id = $1
)
SELECT id, name FROM ancestors ORDER BY depth DESC`

// _userIDColumn is the column every query of the repository scopes by.
const _userIDColumn = "user_id"

// _idColumn is the primary key column of the folders table.
const _idColumn = "id"

// _lockSubtreeQuery locks every folder under a folder, in identifier order, so
// two deletes of overlapping subtrees take the locks in the same order and an
// upload into any of them waits instead of slipping in between.
const _lockSubtreeQuery = `
WITH RECURSIVE tree AS (
    SELECT id, parent_id FROM folders WHERE id = $2 AND user_id = $1
    UNION ALL
    SELECT child.id, child.parent_id
    FROM folders child
    JOIN tree ON child.parent_id = tree.id
    WHERE child.user_id = $1
)
SELECT folders.id
FROM folders
JOIN tree ON tree.id = folders.id
ORDER BY folders.id
FOR UPDATE OF folders`

// Repo -.
type Repo struct {
	*postgres.Postgres
}

// New returns a Folder repository.
func New(pg *postgres.Postgres) repo.FolderRepo {
	return &Repo{pg}
}

// Store -.
func (r *Repo) Store(ctx context.Context, folder *entity.Folder) error {
	sql, args, err := r.Builder.
		Insert("folders").
		Columns("id, user_id, parent_id, name, created_at, updated_at").
		Values(
			folder.ID,
			folder.UserID,
			folder.ParentID,
			folder.Name,
			folder.CreatedAt,
			folder.UpdatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("FolderRepo - Store - r.Builder: %w", err)
	}

	_, err = r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FolderRepo - Store - r.Querier(ctx).Exec: %w", err)
	}

	return nil
}

// Get returns a folder of the account. A folder of another account looks
// missing, so a caller never learns that its identifier exists.
func (r *Repo) Get(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	sql, args, err := r.Builder.
		Select(_folderColumns).
		From("folders").
		Where(sq.Eq{"id": folderID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderRepo - Get - r.Builder: %w", err)
	}

	return scanFolder(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// GetRoot returns the folder Register created for the account.
func (r *Repo) GetRoot(ctx context.Context, userID string) (entity.Folder, error) {
	sql, args, err := r.Builder.
		Select(_folderColumns).
		From("folders").
		Where(sq.Eq{_userIDColumn: userID}).
		Where(sq.Expr("parent_id IS NULL")).
		ToSql()
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderRepo - GetRoot - r.Builder: %w", err)
	}

	return scanFolder(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// LockUser locks the root folder of the account until the transaction ends.
// Every drive mutation takes this lock first, so the mutations of one account
// run one at a time: a folder cannot appear under a subtree while that subtree
// is being deleted, and two names cannot be claimed at once.
func (r *Repo) LockUser(ctx context.Context, userID string) error {
	sql, args, err := r.Builder.
		Select("id").
		From("folders").
		Where(sq.Eq{_userIDColumn: userID}).
		Where(sq.Expr("parent_id IS NULL")).
		Suffix("FOR UPDATE").
		ToSql()
	if err != nil {
		return fmt.Errorf("FolderRepo - LockUser - r.Builder: %w", err)
	}

	var id string

	err = r.Querier(ctx).QueryRow(ctx, sql, args...).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ErrFolderNotFound
		}

		return fmt.Errorf("FolderRepo - LockUser - r.Querier(ctx).QueryRow: %w", err)
	}

	return nil
}

// Lock returns a folder of the account and holds a row lock on it until the
// transaction ends. The name checks of a create or an upload run under this
// lock, so two entries cannot claim the same name at the same time.
func (r *Repo) Lock(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	sql, args, err := r.Builder.
		Select(_folderColumns).
		From("folders").
		Where(sq.Eq{"id": folderID, _userIDColumn: userID}).
		Suffix("FOR UPDATE").
		ToSql()
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderRepo - Lock - r.Builder: %w", err)
	}

	return scanFolder(r.Querier(ctx).QueryRow(ctx, sql, args...))
}

// LockSubtree locks a folder and every folder under it until the transaction
// ends. The walk and the lock are one statement, so the set cannot grow while
// it is being locked; a create or an upload into the subtree has to take the
// lock of its parent and waits. Rows are locked in identifier order, which
// keeps two overlapping deletes from deadlocking.
func (r *Repo) LockSubtree(ctx context.Context, userID, folderID string) error {
	rows, err := r.Querier(ctx).Query(ctx, _lockSubtreeQuery, userID, folderID)
	if err != nil {
		return fmt.Errorf("FolderRepo - LockSubtree - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	locked := 0

	for rows.Next() {
		var id string

		err = rows.Scan(&id)
		if err != nil {
			return fmt.Errorf("FolderRepo - LockSubtree - rows.Scan: %w", err)
		}

		locked++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("FolderRepo - LockSubtree - rows.Err: %w", err)
	}

	if locked == 0 {
		return entity.ErrFolderNotFound
	}

	return nil
}

// List returns the folders directly inside a folder with the number of
// entries each one holds, ordered by name. The modified time is the newest of
// the folder itself and the entries inside it, so a listing shows when the
// contents last changed and not only when the folder was made.
func (r *Repo) List(ctx context.Context, userID, folderID string) ([]entity.FolderItem, error) {
	sql, args, err := r.Builder.
		Select(
			"f.id",
			"f.name",
			"GREATEST("+
				"f.updated_at,"+
				" COALESCE((SELECT max(c.updated_at) FROM folders c WHERE c.parent_id = f.id AND c.user_id = f.user_id), f.updated_at),"+
				" COALESCE((SELECT max(x.updated_at) FROM files x WHERE x.folder_id = f.id AND x.user_id = f.user_id), f.updated_at)"+
				") AS updated_at",
			"(SELECT count(*) FROM folders c WHERE c.parent_id = f.id AND c.user_id = f.user_id)"+
				" + (SELECT count(*) FROM files x WHERE x.folder_id = f.id AND x.user_id = f.user_id) AS item_count",
		).
		From("folders f").
		Where(sq.Eq{"f.user_id": userID, "f.parent_id": folderID}).
		OrderBy("lower(f.name) ASC", "f.id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("FolderRepo - List - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("FolderRepo - List - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	items := make([]entity.FolderItem, 0)

	for rows.Next() {
		var item entity.FolderItem

		err = rows.Scan(&item.ID, &item.Name, &item.UpdatedAt, &item.ItemCount)
		if err != nil {
			return nil, fmt.Errorf("FolderRepo - List - rows.Scan: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FolderRepo - List - rows.Err: %w", err)
	}

	return items, nil
}

// ListAll returns every folder of the account. Roots come first, then
// children in name order. Callers use it to build a destination picker.
func (r *Repo) ListAll(ctx context.Context, userID string) ([]entity.Folder, error) {
	sql, args, err := r.Builder.
		Select(_folderColumns).
		From("folders").
		Where(sq.Eq{_userIDColumn: userID}).
		OrderBy("parent_id IS NULL DESC", "lower(name) ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("FolderRepo - ListAll - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("FolderRepo - ListAll - r.Querier(ctx).Query: %w", err)
	}
	defer rows.Close()

	folders := make([]entity.Folder, 0)

	for rows.Next() {
		var folder entity.Folder

		var parentID string

		err = rows.Scan(
			&folder.ID,
			&folder.UserID,
			&parentID,
			&folder.Name,
			&folder.CreatedAt,
			&folder.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("FolderRepo - ListAll - rows.Scan: %w", err)
		}

		if parentID != "" {
			folder.ParentID = &parentID
		}

		folders = append(folders, folder)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FolderRepo - ListAll - rows.Err: %w", err)
	}

	return folders, nil
}

// Breadcrumb returns the path from the root to the folder, root first. An
// unknown folder returns no rows, which maps to ErrFolderNotFound.
func (r *Repo) Breadcrumb(ctx context.Context, userID, folderID string) ([]entity.FolderRef, error) {
	rows, err := r.Querier(ctx).Query(ctx, _breadcrumbQuery, userID, folderID)
	if err != nil {
		return nil, fmt.Errorf("FolderRepo - Breadcrumb - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	refs := make([]entity.FolderRef, 0)

	for rows.Next() {
		var ref entity.FolderRef

		err = rows.Scan(&ref.ID, &ref.Name)
		if err != nil {
			return nil, fmt.Errorf("FolderRepo - Breadcrumb - rows.Scan: %w", err)
		}

		refs = append(refs, ref)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FolderRepo - Breadcrumb - rows.Err: %w", err)
	}

	if len(refs) == 0 {
		return nil, entity.ErrFolderNotFound
	}

	return refs, nil
}

// NameTaken reports whether a folder of the account already carries the name
// in the same parent. The comparison ignores case, as the unique index does.
func (r *Repo) NameTaken(ctx context.Context, userID, parentID, name string) (bool, error) {
	builder := r.Builder.
		Select("count(*)").
		From("folders").
		Where(sq.Eq{_userIDColumn: userID}).
		Where(sq.Expr("lower(name) = lower(?)", name))

	var sql string

	var args []any

	var err error

	if parentID == "" {
		sql, args, err = builder.Where(sq.Expr("parent_id IS NULL")).ToSql()
	} else {
		sql, args, err = builder.Where(sq.Eq{"parent_id": parentID}).ToSql()
	}

	if err != nil {
		return false, fmt.Errorf("FolderRepo - NameTaken - r.Builder: %w", err)
	}

	var count int

	err = r.Querier(ctx).QueryRow(ctx, sql, args...).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("FolderRepo - NameTaken - r.Querier(ctx).QueryRow: %w", err)
	}

	return count > 0, nil
}

// Rename stores the name a use case already resolved. A missing folder maps to
// ErrFolderNotFound.
func (r *Repo) Rename(ctx context.Context, folder *entity.Folder) error {
	sql, args, err := r.Builder.
		Update("folders").
		Set("name", folder.Name).
		Set("updated_at", folder.UpdatedAt).
		Where(sq.Eq{"id": folder.ID, _userIDColumn: folder.UserID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("FolderRepo - Rename - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FolderRepo - Rename - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrFolderNotFound
	}

	return nil
}

// Move changes the parent of a folder. A missing folder maps to
// ErrFolderNotFound; a name that already exists in the destination maps to
// ErrEntryAlreadyExists through the unique index on siblings.
func (r *Repo) Move(ctx context.Context, userID, folderID, parentID string) error {
	sql, args, err := r.Builder.
		Update("folders").
		Set("parent_id", parentID).
		Set("updated_at", time.Now().UTC()).
		Where(sq.Eq{_idColumn: folderID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("FolderRepo - Move - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return entity.ErrEntryAlreadyExists
		}

		return fmt.Errorf("FolderRepo - Move - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrFolderNotFound
	}

	return nil
}

// Delete removes a folder of the account. The foreign keys cascade the
// deletion through the subtree and the files it holds.
func (r *Repo) Delete(ctx context.Context, userID, folderID string) error {
	sql, args, err := r.Builder.
		Delete("folders").
		Where(sq.Eq{"id": folderID, _userIDColumn: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("FolderRepo - Delete - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("FolderRepo - Delete - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrFolderNotFound
	}

	return nil
}

// scanFolder reads one folder row and rebuilds the nullable parent after the
// scan.
func scanFolder(row pgx.Row) (entity.Folder, error) {
	var (
		folder   entity.Folder
		parentID string
	)

	err := row.Scan(
		&folder.ID,
		&folder.UserID,
		&parentID,
		&folder.Name,
		&folder.CreatedAt,
		&folder.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Folder{}, entity.ErrFolderNotFound
		}

		return entity.Folder{}, fmt.Errorf("FolderRepo - scanFolder - row.Scan: %w", err)
	}

	if parentID != "" {
		folder.ParentID = &parentID
	}

	return folder, nil
}
