package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// recordingLogger captures the lines the seeder asked it to log.
type recordingLogger struct {
	silentLogger
	lines []string
}

func (r *recordingLogger) Info(message string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(message, args...))
}

func (r *recordingLogger) Error(message any, _ ...any) {
	r.lines = append(r.lines, fmt.Sprint(message))
}

// errSeedFailed is the failure the fake seeder reports.
var errSeedFailed = errors.New("seed failed")

// recordingSeedUser records the accounts seedUsers asked it to create and
// answers with the canned error.
type recordingSeedUser struct {
	usecase.User

	err   error
	calls []entity.User
}

func (r *recordingSeedUser) SeedUser(
	_ context.Context,
	username, email, password string,
	role entity.UserRole,
	status entity.UserStatus,
) (entity.User, error) {
	r.calls = append(r.calls, entity.User{
		Username:     username,
		Email:        email,
		PasswordHash: password,
		Role:         role,
		Status:       status,
	})

	return entity.User{}, r.err
}

// seedConfig returns the config with the seed section filled in.
func seedConfig(admin, demo bool) *config.Config {
	cfg := &config.Config{}

	if admin {
		cfg.Seed.AdminEmail = "admin@example.com"
		cfg.Seed.AdminUsername = "admin"
		cfg.Seed.AdminPassword = "Admin123!"
	}

	if demo {
		cfg.Seed.DemoEmail = "demo@example.com"
		cfg.Seed.DemoUsername = "demo"
		cfg.Seed.DemoPassword = "Demo123!"
	}

	return cfg
}

func TestSeedUsersCreatesBothConfiguredAccounts(t *testing.T) {
	t.Parallel()

	users := &recordingSeedUser{}
	logs := &recordingLogger{}

	seedUsers(context.Background(), users, seedConfig(true, true), logs)

	if len(users.calls) != 2 {
		t.Fatalf("SeedUser calls = %d, want 2", len(users.calls))
	}

	admin := users.calls[0]

	if admin.Email != "admin@example.com" || admin.PasswordHash != "Admin123!" {
		t.Errorf("admin call = %+v, want the admin credentials", admin)
	}

	if admin.Role != entity.UserRoleAdmin {
		t.Errorf("admin role = %q, want %q", admin.Role, entity.UserRoleAdmin)
	}

	if !admin.Status.CanSignIn() {
		t.Errorf("admin status = %q, want an account that may sign in", admin.Status)
	}

	demo := users.calls[1]

	if demo.Email != "demo@example.com" || demo.PasswordHash != "Demo123!" {
		t.Errorf("demo call = %+v, want the demo credentials", demo)
	}

	if demo.Role != entity.UserRoleUser {
		t.Errorf("demo role = %q, want %q", demo.Role, entity.UserRoleUser)
	}

	if !demo.Status.CanSignIn() {
		t.Errorf("demo status = %q, want an account that may sign in", demo.Status)
	}
}

func TestSeedUsersSkipsAccountsThatAlreadyExist(t *testing.T) {
	t.Parallel()

	users := &recordingSeedUser{err: entity.ErrEmailAlreadyExists}
	logs := &recordingLogger{}

	seedUsers(context.Background(), users, seedConfig(true, true), logs)

	if len(users.calls) != 2 {
		t.Fatalf("SeedUser calls = %d, want 2", len(users.calls))
	}

	if len(logs.lines) != 2 {
		t.Fatalf("log lines = %d, want one per skipped account", len(logs.lines))
	}
}

func TestSeedUsersSeedsOnlyFullyConfiguredAccounts(t *testing.T) {
	t.Parallel()

	demoMissingPassword := seedConfig(true, false)
	demoMissingPassword.Seed.DemoEmail = "demo@example.com"
	demoMissingPassword.Seed.DemoUsername = "demo"

	users := &recordingSeedUser{}
	seedUsers(context.Background(), users, demoMissingPassword, &recordingLogger{})

	if len(users.calls) != 1 {
		t.Fatalf("SeedUser calls = %d, want 1", len(users.calls))
	}

	if users.calls[0].Email != "admin@example.com" {
		t.Errorf("seeded email = %q, want the admin", users.calls[0].Email)
	}
}

func TestSeedUsersSeedsNothingWithoutConfiguration(t *testing.T) {
	t.Parallel()

	users := &recordingSeedUser{}
	seedUsers(context.Background(), users, seedConfig(false, false), &recordingLogger{})

	if len(users.calls) != 0 {
		t.Fatalf("SeedUser calls = %d, want 0", len(users.calls))
	}
}

func TestSeedUsersLogsAFailureAndKeepsSeeding(t *testing.T) {
	t.Parallel()

	users := &recordingSeedUser{err: errSeedFailed}
	logs := &recordingLogger{}

	seedUsers(context.Background(), users, seedConfig(true, true), logs)

	if len(users.calls) != 2 {
		t.Fatalf("SeedUser calls = %d, want the seeder to keep going", len(users.calls))
	}

	if len(logs.lines) != 2 {
		t.Fatalf("log lines = %d, want one per failed account", len(logs.lines))
	}
}

// compile-time guard: the recording logger stays a logger.Interface.
var _ logger.Interface = (*recordingLogger)(nil)
