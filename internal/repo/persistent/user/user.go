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

// Update rewrites the editable fields of an account from the account the use
// case handed in, so the caller has already resolved which parts change. A
// name or an email another account already holds maps to the sentinel
// uniqueViolation names, a missing account to ErrUserNotFound.
func (r *Repo) Update(ctx context.Context, user *entity.User) error {
	sql, args, err := r.Builder.
		Update("users").
		Set("username", user.Username).
		Set("email", user.Email).
		Set("password_hash", user.PasswordHash).
		Set("role", user.Role).
		Set("updated_at", user.UpdatedAt).
		Where(sq.Eq{"id": user.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("UserRepo - Update - r.Builder: %w", err)
	}

	tag, err := r.Querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uniqueViolation(pgErr)
		}

		return fmt.Errorf("UserRepo - Update - r.Querier(ctx).Exec: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return entity.ErrUserNotFound
	}

	return nil
}

// _userFirstPage is the page number the account list starts at.
const _userFirstPage = 1

// List returns one page of accounts in the order the filter asks for, as the
// read model a list renders, plus how many accounts match the filter in total.
// The password hash is not selected, so it never leaves the database.
func (r *Repo) List(ctx context.Context, filter *entity.UserFilter) (entity.UserList, error) {
	where := listWhere(filter)

	countSQL, countArgs, err := r.Builder.
		Select("count(*)").
		From("users").
		Where(where).
		ToSql()
	if err != nil {
		return entity.UserList{}, fmt.Errorf("UserRepo - List - r.Builder: %w", err)
	}

	var total int

	err = r.Querier(ctx).QueryRow(ctx, countSQL, countArgs...).Scan(&total)
	if err != nil {
		return entity.UserList{}, fmt.Errorf("UserRepo - List - r.Querier(ctx).QueryRow: %w", err)
	}

	limit := max(filter.Limit, 0)

	offset := max((filter.Page-_userFirstPage)*filter.Limit, 0)

	pageSQL, pageArgs, err := r.Builder.
		Select("id, username, email, role, created_at, updated_at").
		From("users").
		Where(where).
		OrderBy(listOrder(filter)...).
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return entity.UserList{}, fmt.Errorf("UserRepo - List - r.Builder: %w", err)
	}

	rows, err := r.Querier(ctx).Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		return entity.UserList{}, fmt.Errorf("UserRepo - List - r.Querier(ctx).Query: %w", err)
	}

	defer rows.Close()

	users := make([]entity.UserListItem, 0, filter.Limit)

	for rows.Next() {
		var user entity.UserListItem

		err = rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return entity.UserList{}, fmt.Errorf("UserRepo - List - rows.Scan: %w", err)
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return entity.UserList{}, fmt.Errorf("UserRepo - List - rows.Err: %w", err)
	}

	return entity.UserList{Items: users, Total: total}, nil
}

// listOrder maps the order of the filter to the columns of the list. The
// domain has already kept the filter to the columns it knows, so the values
// are column names and never text from the caller. The identifier breaks a
// tie, so a page never repeats or skips a row between two requests.
func listOrder(filter *entity.UserFilter) []string {
	var column string

	switch filter.Sort {
	case entity.UserSortUsername:
		column = "username"
	case entity.UserSortEmail:
		column = "email"
	case entity.UserSortRole:
		column = "role"
	case entity.UserSortCreatedAt:
		column = "created_at"
	case entity.UserSortUpdatedAt:
		column = "updated_at"
	default:
		column = "created_at"
	}

	var direction string

	switch filter.Dir {
	case entity.UserSortAscending:
		direction = "ASC"
	case entity.UserSortDescending:
		direction = "DESC"
	default:
		direction = "DESC"
	}

	return []string{column + " " + direction, "id ASC"}
}

// listWhere builds the conditions the account list filters by: the query
// matches the username or the email, the role matches exactly, and the day
// matches the creation or the last update. It returns nil when the filter is
// empty, so an unfiltered query stays without a WHERE clause.
func listWhere(filter *entity.UserFilter) sq.Sqlizer {
	conds := []sq.Sqlizer{}

	if filter.Query != "" {
		pattern := "%" + escapeLike(filter.Query) + "%"

		conds = append(conds, sq.Or{
			sq.Expr("username ILIKE ? ESCAPE '\\'", pattern),
			sq.Expr("email ILIKE ? ESCAPE '\\'", pattern),
		})
	}

	if filter.Role != "" {
		conds = append(conds, sq.Eq{"role": filter.Role})
	}

	if filter.On != nil {
		conds = append(conds, sq.Or{
			sq.Expr("created_at::date = ?::date", *filter.On),
			sq.Expr("updated_at::date = ?::date", *filter.On),
		})
	}

	if len(conds) == 0 {
		return nil
	}

	return sq.And(conds)
}

// escapeLike neutralizes the LIKE wildcards in what the admin typed, so a
// search for "100%" matches that literal text instead of everything.
func escapeLike(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	return replacer.Replace(query)
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
