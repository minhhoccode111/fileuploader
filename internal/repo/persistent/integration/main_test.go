//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	fileRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/file"
	folderRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/folder"
	userRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/user"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
)

var testPG *postgres.Postgres

func TestMain(m *testing.M) {
	adminURL := os.Getenv("TEST_PG_ADMIN_URL")
	testURL := os.Getenv("TEST_PG_URL")
	if adminURL == "" || testURL == "" {
		log.Fatal("TEST_PG_ADMIN_URL and TEST_PG_URL must be set; run make test-integration")
	}

	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		log.Fatalf("connect to the test admin database: %v", err)
	}

	dropTestDatabase(ctx, admin)
	createTestDatabase(ctx, admin)
	applyMigrations(testURL)

	testPG, err = postgres.New(testURL)
	if err != nil {
		log.Fatalf("connect to the test database: %v", err)
	}

	code := m.Run()

	testPG.Close()
	dropTestDatabase(ctx, admin)

	if err := admin.Close(ctx); err != nil {
		log.Printf("close the admin connection: %v", err)
	}

	os.Exit(code)
}

func dropTestDatabase(ctx context.Context, admin *pgx.Conn) {
	_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS fileuploader_test WITH (FORCE)")
	if err != nil {
		log.Fatalf("drop the test database: %v", err)
	}
}

func createTestDatabase(ctx context.Context, admin *pgx.Conn) {
	_, err := admin.Exec(ctx, "CREATE DATABASE fileuploader_test")
	if err != nil {
		log.Fatalf("create the test database: %v", err)
	}
}

func applyMigrations(testURL string) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("resolve the integration test path")
	}

	migrations := filepath.Join(filepath.Dir(current), "../../../../migrations")

	mig, err := migrate.New("file://"+migrations, testURL)
	if err != nil {
		log.Fatalf("open migrations: %v", err)
	}

	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("apply migrations: %v", err)
	}

	sourceErr, dbErr := mig.Close()
	if sourceErr != nil || dbErr != nil {
		log.Fatalf("close migrations: source=%v database=%v", sourceErr, dbErr)
	}
}

func testPostgres(t *testing.T) *postgres.Postgres {
	t.Helper()

	if testPG == nil {
		t.Fatal("test Postgres is not initialized")
	}

	return testPG
}

func seedUser(t *testing.T, username, email string) entity.User {
	t.Helper()

	now := time.Now().UTC()
	user := entity.User{
		ID:           uuid.NewString(),
		Username:     username,
		Email:        email,
		PasswordHash: "hash",
		Role:         entity.UserRoleUser,
		Status:       entity.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := userRepo.New(testPostgres(t)).Store(context.Background(), &user); err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}

	return user
}

func seedRoot(t *testing.T, userID string) entity.Folder {
	t.Helper()

	now := time.Now().UTC()
	folder := entity.Folder{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      entity.RootFolderName,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := folderRepo.New(testPostgres(t)).Store(context.Background(), &folder); err != nil {
		t.Fatalf("seed root for %q: %v", userID, err)
	}

	return folder
}

func seedFolder(t *testing.T, userID, parentID, name string) entity.Folder {
	t.Helper()

	now := time.Now().UTC()
	folder := entity.Folder{
		ID:        uuid.NewString(),
		UserID:    userID,
		ParentID:  &parentID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := folderRepo.New(testPostgres(t)).Store(context.Background(), &folder); err != nil {
		t.Fatalf("seed folder %q: %v", name, err)
	}

	return folder
}

func seedFile(t *testing.T, userID, folderID, name, key string, size int64) entity.File {
	t.Helper()

	now := time.Now().UTC()
	file := entity.File{
		ID:          uuid.NewString(),
		UserID:      userID,
		FolderID:    folderID,
		Name:        name,
		ContentType: "text/plain",
		StorageKey:  key,
		Size:        size,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := fileRepo.New(testPostgres(t)).Store(context.Background(), &file); err != nil {
		t.Fatalf("seed file %q: %v", name, err)
	}

	return file
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.NewString())
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
