package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"golang.org/x/crypto/bcrypt"
)

// errStoreFailed is the failure the fake repository reports.
var errStoreFailed = errors.New("store failed")

// fakeUserRepo records what it was asked to store.
type fakeUserRepo struct {
	stored  *entity.User
	user    entity.User
	role    entity.UserRole
	roleSet bool
	err     error
}

func (f *fakeUserRepo) Store(_ context.Context, user *entity.User) error {
	if f.err != nil {
		return f.err
	}

	f.stored = user

	return nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.err
}

func (f *fakeUserRepo) GetByEmail(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.err
}

func (f *fakeUserRepo) List(_ context.Context) ([]entity.UserListItem, error) {
	return nil, f.err
}

func (f *fakeUserRepo) UpdateRole(_ context.Context, _ string, role entity.UserRole) error {
	if f.err != nil {
		return f.err
	}

	f.role = role
	f.roleSet = true

	return nil
}

// fakeSessionRepo records the session it was asked to store.
type fakeSessionRepo struct {
	stored *entity.Session
	ended  []string
}

func (f *fakeSessionRepo) Store(_ context.Context, _ string, session entity.Session, _ time.Duration) error {
	f.stored = &session

	return nil
}

func (f *fakeSessionRepo) Get(_ context.Context, _ string) (entity.Session, error) {
	return entity.Session{}, nil
}

func (f *fakeSessionRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSessionRepo) DeleteUserSessions(_ context.Context, userID string) error {
	f.ended = append(f.ended, userID)

	return nil
}

// fakeUnitOfWork records how the unit of work ended.
type fakeUnitOfWork struct {
	called     bool
	committed  bool
	rolledBack bool
}

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	f.called = true

	err := fn(ctx)
	if err != nil {
		f.rolledBack = true

		return err
	}

	f.committed = true

	return nil
}

func TestRegisterStoresTheUserInsideTheUnitOfWork(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{}
	uow := &fakeUnitOfWork{}
	uc := New(repo, &fakeSessionRepo{}, uow, time.Hour)

	user, err := uc.Register(context.Background(), "john", "john@example.com", "password")
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	if !uow.called {
		t.Error("Register() did not run inside the unit of work")
	}

	if !uow.committed {
		t.Error("Register() did not commit the unit of work")
	}

	if repo.stored == nil {
		t.Fatal("Register() did not store the user")
	}

	if repo.stored.ID != user.ID {
		t.Errorf("stored user ID = %q, want %q", repo.stored.ID, user.ID)
	}
}

func TestRegisterRollsBackWhenTheRepositoryFails(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{err: errStoreFailed}
	uow := &fakeUnitOfWork{}
	uc := New(repo, &fakeSessionRepo{}, uow, time.Hour)

	_, err := uc.Register(context.Background(), "john", "john@example.com", "password")
	if !errors.Is(err, errStoreFailed) {
		t.Fatalf("Register() error = %v, want %v", err, errStoreFailed)
	}

	if !uow.rolledBack {
		t.Error("Register() did not roll back the unit of work")
	}

	if uow.committed {
		t.Error("Register() committed a failed unit of work")
	}
}

func TestLoginStoresTheRoleInTheSession(t *testing.T) {
	t.Parallel()

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error = %v, want nil", err)
	}

	repo := &fakeUserRepo{user: entity.User{ID: "1", Role: entity.UserRoleAdmin, PasswordHash: string(hash)}}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeUnitOfWork{}, time.Hour)

	_, _, err = uc.Login(context.Background(), "john@example.com", "password")
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	if sessions.stored == nil {
		t.Fatal("Login() did not store the session")
	}

	if sessions.stored.Role != entity.UserRoleAdmin {
		t.Errorf("stored role = %q, want %q", sessions.stored.Role, entity.UserRoleAdmin)
	}

	if sessions.stored.UserID != "1" {
		t.Errorf("stored user ID = %q, want %q", sessions.stored.UserID, "1")
	}
}

func TestSetRoleUpdatesTheRoleAndEndsTheSessions(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{user: entity.User{ID: "1", Email: "john@example.com"}}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeUnitOfWork{}, time.Hour)

	err := uc.SetRole(context.Background(), "john@example.com", entity.UserRoleAdmin)
	if err != nil {
		t.Fatalf("SetRole() error = %v, want nil", err)
	}

	if !repo.roleSet {
		t.Fatal("SetRole() did not update the role")
	}

	if repo.role != entity.UserRoleAdmin {
		t.Errorf("updated role = %q, want %q", repo.role, entity.UserRoleAdmin)
	}

	if len(sessions.ended) != 1 || sessions.ended[0] != "1" {
		t.Errorf("ended sessions = %v, want the sessions of user %q", sessions.ended, "1")
	}
}

func TestSetRoleRejectsAnUnknownRole(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{user: entity.User{ID: "1"}}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeUnitOfWork{}, time.Hour)

	err := uc.SetRole(context.Background(), "john@example.com", entity.UserRole("root"))
	if !errors.Is(err, entity.ErrInvalidRole) {
		t.Fatalf("SetRole() error = %v, want %v", err, entity.ErrInvalidRole)
	}

	if repo.roleSet {
		t.Error("SetRole() updated the role of an unknown role")
	}
}

func TestSetRoleReportsAnUnknownAccount(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{err: entity.ErrUserNotFound}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeUnitOfWork{}, time.Hour)

	err := uc.SetRole(context.Background(), "nobody@example.com", entity.UserRoleAdmin)
	if !errors.Is(err, entity.ErrUserNotFound) {
		t.Fatalf("SetRole() error = %v, want %v", err, entity.ErrUserNotFound)
	}

	if len(sessions.ended) != 0 {
		t.Errorf("ended sessions = %v, want none", sessions.ended)
	}
}
