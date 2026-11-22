//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	folderRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/folder"
	userRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/user"
)

var errBoom = errors.New("boom")

func TestUnitOfWorkCommits(t *testing.T) {
	t.Parallel()

	pg := testPostgres(t)
	user := testUser()

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		return userRepo.New(pg).Store(ctx, &user)
	})
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	if !userExists(t, user.ID) {
		t.Error("Do() did not commit the user")
	}
}

func TestUnitOfWorkRollsBack(t *testing.T) {
	t.Parallel()

	pg := testPostgres(t)
	user := testUser()

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		if storeErr := userRepo.New(pg).Store(ctx, &user); storeErr != nil {
			return storeErr
		}

		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do() error = %v, want %v", err, errBoom)
	}

	if userExists(t, user.ID) {
		t.Error("Do() committed a rolled-back user")
	}
}

func TestNestedUnitOfWorkReusesTheTransaction(t *testing.T) {
	t.Parallel()

	pg := testPostgres(t)
	user := testUser()
	folder := entity.Folder{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Name:      entity.RootFolderName,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		if storeErr := userRepo.New(pg).Store(ctx, &user); storeErr != nil {
			return storeErr
		}

		return pg.Do(ctx, func(ctx context.Context) error {
			return folderRepo.New(pg).Store(ctx, &folder)
		})
	})
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	if !userExists(t, user.ID) || !folderExists(t, folder.ID) {
		t.Error("nested Do() did not commit both rows")
	}
}

func TestNestedUnitOfWorkErrorRollsBackTheOuter(t *testing.T) {
	t.Parallel()

	pg := testPostgres(t)
	user := testUser()

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		if storeErr := userRepo.New(pg).Store(ctx, &user); storeErr != nil {
			return storeErr
		}

		return pg.Do(ctx, func(context.Context) error {
			return errBoom
		})
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do() error = %v, want %v", err, errBoom)
	}

	if userExists(t, user.ID) {
		t.Error("nested Do() committed a row from a failed outer transaction")
	}
}

func TestMigrationsApplied(t *testing.T) {
	t.Parallel()

	pg := testPostgres(t)

	var count int
	if err := pg.Querier(context.Background()).
		QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").
		Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}

	if count == 0 {
		t.Error("schema_migrations is empty")
	}

	var usersTable bool
	if err := pg.Querier(context.Background()).
		QueryRow(context.Background(), "SELECT to_regclass('public.users') IS NOT NULL").
		Scan(&usersTable); err != nil {
		t.Fatalf("query users table: %v", err)
	}

	if !usersTable {
		t.Error("users table is missing")
	}
}

func testUser() entity.User {
	now := time.Now().UTC()
	name := uniqueName("uow")

	return entity.User{
		ID:           uuid.NewString(),
		Username:     name,
		Email:        name + "@example.com",
		PasswordHash: "hash",
		Role:         entity.UserRoleUser,
		Status:       entity.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func userExists(t *testing.T, userID string) bool {
	t.Helper()

	var exists bool
	err := testPostgres(t).Querier(context.Background()).
		QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", userID).
		Scan(&exists)
	if err != nil {
		t.Fatalf("query user %q: %v", userID, err)
	}

	return exists
}

func folderExists(t *testing.T, folderID string) bool {
	t.Helper()

	var exists bool
	err := testPostgres(t).Querier(context.Background()).
		QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM folders WHERE id = $1)", folderID).
		Scan(&exists)
	if err != nil {
		t.Fatalf("query folder %q: %v", folderID, err)
	}

	return exists
}
