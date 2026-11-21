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

// fakeUserRepo records what it was asked to store and the filter it was
// asked to list with.
type fakeUserRepo struct {
	stored  *entity.User
	listed  entity.UserFilter
	user    entity.User
	items   []entity.UserListItem
	role    entity.UserRole
	roleSet bool
	updated *entity.User
	err     error

	limitsSet    bool
	limitsUserID string
	quota        int64
	uploadRate   int64
	downloadRate int64
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

func (f *fakeUserRepo) List(_ context.Context, filter *entity.UserFilter) (entity.UserList, error) {
	f.listed = *filter

	return entity.UserList{Items: f.items, Total: len(f.items)}, f.err
}

func (f *fakeUserRepo) UpdateRole(_ context.Context, _ string, role entity.UserRole) error {
	if f.err != nil {
		return f.err
	}

	f.role = role
	f.roleSet = true

	return nil
}

func (f *fakeUserRepo) UpdateStatus(_ context.Context, _ string, _ entity.UserStatus) error {
	if f.err != nil {
		return f.err
	}

	return nil
}

func (f *fakeUserRepo) Update(_ context.Context, user *entity.User) error {
	if f.err != nil {
		return f.err
	}

	f.updated = user

	return nil
}

func (f *fakeUserRepo) SetUserLimits(_ context.Context, userID string, quota, uploadRate, downloadRate int64) error {
	if f.err != nil {
		return f.err
	}

	f.limitsSet = true
	f.limitsUserID = userID
	f.quota = quota
	f.uploadRate = uploadRate
	f.downloadRate = downloadRate

	return nil
}

func (f *fakeUserRepo) AdjustStorageUsed(_ context.Context, _ string, _ int64) error {
	return f.err
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

// fakeFolderRepo records the root folder Register was asked to create.
type fakeFolderRepo struct {
	stored *entity.Folder
	err    error
}

func (f *fakeFolderRepo) Store(_ context.Context, folder *entity.Folder) error {
	if f.err != nil {
		return f.err
	}

	f.stored = folder

	return nil
}

func (f *fakeFolderRepo) Get(_ context.Context, _, _ string) (entity.Folder, error) {
	return entity.Folder{}, f.err
}

func (f *fakeFolderRepo) Lock(_ context.Context, _, _ string) (entity.Folder, error) {
	return entity.Folder{}, f.err
}

func (f *fakeFolderRepo) LockUser(_ context.Context, _ string) error {
	return f.err
}

func (f *fakeFolderRepo) LockSubtree(_ context.Context, _, _ string) error {
	return f.err
}

func (f *fakeFolderRepo) GetRoot(_ context.Context, _ string) (entity.Folder, error) {
	return entity.Folder{}, f.err
}

func (f *fakeFolderRepo) ListPage(
	_ context.Context,
	_, _, _ string,
	_ *entity.EntryCursor,
	_ int,
) ([]entity.FolderItem, error) {
	return nil, f.err
}

func (f *fakeFolderRepo) Paths(
	_ context.Context,
	_ string,
	_ []string,
) (map[string][]entity.FolderRef, error) {
	return nil, f.err
}

func (f *fakeFolderRepo) Breadcrumb(_ context.Context, _, _ string) ([]entity.FolderRef, error) {
	return nil, f.err
}

func (f *fakeFolderRepo) NameTaken(_ context.Context, _, _, _ string) (bool, error) {
	return false, f.err
}

func (f *fakeFolderRepo) Rename(_ context.Context, _ *entity.Folder) error {
	return f.err
}

func (f *fakeFolderRepo) Move(_ context.Context, _, _, _ string) error {
	return f.err
}

func (f *fakeFolderRepo) Delete(_ context.Context, _, _ string) error {
	return f.err
}

func (f *fakeFolderRepo) ListAll(_ context.Context, _ string) ([]entity.Folder, error) {
	return nil, f.err
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

func TestListUsersClampsTheFilterBeforeItLists(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{}
	uc := New(repo, &fakeSessionRepo{}, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

	_, err := uc.ListUsers(context.Background(), &entity.UserFilter{
		Query: "  john  ",
		Role:  entity.UserRole("root"),
		Limit: 3,
		Page:  0,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v, want nil", err)
	}

	if repo.listed.Query != "john" {
		t.Errorf("listed query = %q, want %q", repo.listed.Query, "john")
	}

	if repo.listed.Role != "" {
		t.Errorf("listed role = %q, want it empty", repo.listed.Role)
	}

	if repo.listed.Limit != 10 {
		t.Errorf("listed limit = %d, want %d", repo.listed.Limit, 10)
	}

	if repo.listed.Page != 1 {
		t.Errorf("listed page = %d, want %d", repo.listed.Page, 1)
	}
}

func TestRegisterStoresTheUserInsideTheUnitOfWork(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{}
	folders := &fakeFolderRepo{}
	uow := &fakeUnitOfWork{}
	uc := New(repo, &fakeSessionRepo{}, folders, uow, time.Hour)

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

	if folders.stored == nil {
		t.Fatal("Register() did not create the root folder")
	}

	if folders.stored.Name != entity.RootFolderName {
		t.Errorf("root folder name = %q, want %q", folders.stored.Name, entity.RootFolderName)
	}

	if folders.stored.UserID != user.ID {
		t.Errorf("root folder user ID = %q, want %q", folders.stored.UserID, user.ID)
	}
}

func TestRegisterRollsBackWhenTheRepositoryFails(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{err: errStoreFailed}
	uow := &fakeUnitOfWork{}
	uc := New(repo, &fakeSessionRepo{}, &fakeFolderRepo{}, uow, time.Hour)

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

	repo := &fakeUserRepo{user: entity.User{ID: "1", Role: entity.UserRoleAdmin, Status: entity.UserStatusActive, PasswordHash: string(hash)}}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

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
	uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

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
	uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

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
	uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

	err := uc.SetRole(context.Background(), "nobody@example.com", entity.UserRoleAdmin)
	if !errors.Is(err, entity.ErrUserNotFound) {
		t.Fatalf("SetRole() error = %v, want %v", err, entity.ErrUserNotFound)
	}

	if len(sessions.ended) != 0 {
		t.Errorf("ended sessions = %v, want none", sessions.ended)
	}
}

// UpdateUser saves the fields it was given, and hashing the password only
// happens when the caller brought a new one.
func TestUpdateUserSavesTheFields(t *testing.T) {
	t.Parallel()

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error = %v, want nil", err)
	}

	repo := &fakeUserRepo{user: entity.User{
		ID:           "1",
		Username:     "john",
		Email:        "john@example.com",
		PasswordHash: string(hash),
		Role:         entity.UserRoleUser,
	}}
	sessions := &fakeSessionRepo{}
	uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

	user, sessionsEnded, err := uc.UpdateUser(
		context.Background(), "1", "johndoe", "doe@example.com", entity.UserRoleUser, "",
	)
	if err != nil {
		t.Fatalf("UpdateUser() error = %v, want nil", err)
	}

	if repo.updated == nil {
		t.Fatal("UpdateUser() did not store the account")
	}

	if repo.updated.Username != "johndoe" || repo.updated.Email != "doe@example.com" {
		t.Errorf("updated account = %s <%s>, want johndoe <doe@example.com>", repo.updated.Username, repo.updated.Email)
	}

	if sessionsEnded {
		t.Error("UpdateUser() ended the sessions of a rename")
	}

	if len(sessions.ended) != 0 {
		t.Errorf("ended sessions = %v, want none", sessions.ended)
	}

	if user.Role != entity.UserRoleUser {
		t.Errorf("updated role = %q, want %q", user.Role, entity.UserRoleUser)
	}
}

// A role or a password change has to end the live sessions: the session holds
// a snapshot of the role and the credentials behind it, so both go stale on
// such a change. A rename changes nothing the session holds.
func TestUpdateUserEndsTheSessionsOnARoleOrAPasswordChange(t *testing.T) {
	t.Parallel()

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error = %v, want nil", err)
	}

	cases := []struct {
		name string
		role entity.UserRole
		pass string
	}{
		{name: "a role change", role: entity.UserRoleAdmin, pass: ""},
		{name: "a password change", role: entity.UserRoleUser, pass: "newpass1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{user: entity.User{ID: "1", Role: entity.UserRoleUser, PasswordHash: string(hash)}}
			sessions := &fakeSessionRepo{}
			uc := New(repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

			_, sessionsEnded, err := uc.UpdateUser(
				context.Background(), "1", "john", "john@example.com", tc.role, tc.pass,
			)
			if err != nil {
				t.Fatalf("UpdateUser() error = %v, want nil", err)
			}

			if !sessionsEnded {
				t.Error("UpdateUser() kept the sessions of a role or a password change")
			}

			if len(sessions.ended) != 1 || sessions.ended[0] != "1" {
				t.Errorf("ended sessions = %v, want the sessions of user %q", sessions.ended, "1")
			}
		})
	}
}

func TestUpdateUserRejectsAnUnknownRoleOrAccount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		repo   fakeUserRepo
		role   entity.UserRole
		errors error
	}{
		{
			name:   "an unknown role",
			repo:   fakeUserRepo{},
			role:   entity.UserRole("root"),
			errors: entity.ErrInvalidRole,
		},
		{
			name:   "an unknown account",
			repo:   fakeUserRepo{err: entity.ErrUserNotFound},
			role:   entity.UserRoleUser,
			errors: entity.ErrUserNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sessions := &fakeSessionRepo{}
			uc := New(&tc.repo, sessions, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

			user, sessionsEnded, err := uc.UpdateUser(
				context.Background(), "1", "john", "john@example.com", tc.role, "",
			)
			if !errors.Is(err, tc.errors) {
				t.Fatalf("UpdateUser() error = %v, want %v", err, tc.errors)
			}

			if user != (entity.User{}) {
				t.Errorf("UpdateUser() user = %+v, want the empty user", user)
			}

			if sessionsEnded {
				t.Error("UpdateUser() ended the sessions of a failed update")
			}

			if len(sessions.ended) != 0 {
				t.Errorf("ended sessions = %v, want none", sessions.ended)
			}
		})
	}
}

func TestUpdateUserKeepsThePasswordWhenNoneCameIn(t *testing.T) {
	t.Parallel()

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error = %v, want nil", err)
	}

	repo := &fakeUserRepo{user: entity.User{
		ID:           "1",
		PasswordHash: string(hash),
		Role:         entity.UserRoleUser,
	}}
	uc := New(repo, &fakeSessionRepo{}, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

	_, _, err = uc.UpdateUser(context.Background(), "1", "john", "john@example.com", entity.UserRoleUser, "")
	if err != nil {
		t.Fatalf("UpdateUser() error = %v, want nil", err)
	}

	if repo.updated.PasswordHash != string(hash) {
		t.Errorf("updated password hash = %q, want it unchanged", repo.updated.PasswordHash)
	}
}

func TestUpdateUserReplacesThePassword(t *testing.T) {
	t.Parallel()

	repo := &fakeUserRepo{user: entity.User{ID: "1", PasswordHash: "old", Role: entity.UserRoleUser}}
	uc := New(repo, &fakeSessionRepo{}, &fakeFolderRepo{}, &fakeUnitOfWork{}, time.Hour)

	_, _, err := uc.UpdateUser(context.Background(), "1", "john", "john@example.com", entity.UserRoleUser, "newpass1")
	if err != nil {
		t.Fatalf("UpdateUser() error = %v, want nil", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(repo.updated.PasswordHash), []byte("newpass1"))
	if err != nil {
		t.Errorf("bcrypt.CompareHashAndPassword() error = %v, want nil", err)
	}
}
