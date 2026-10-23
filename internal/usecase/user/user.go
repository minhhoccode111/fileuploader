package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"golang.org/x/crypto/bcrypt"
)

// _sessionIDBytes is the size of the random session identifier. 32 bytes (256
// bits) makes guessing a live session infeasible.
const _sessionIDBytes = 32

// UseCase -.
type UseCase struct {
	repo       repo.UserRepo
	sessions   repo.SessionRepo
	uow        repo.UnitOfWork
	sessionTTL time.Duration
}

// New returns a User usecase.
func New(r repo.UserRepo, s repo.SessionRepo, u repo.UnitOfWork, sessionTTL time.Duration) usecase.User {
	return &UseCase{
		repo:       r,
		sessions:   s,
		uow:        u,
		sessionTTL: sessionTTL,
	}
}

// Register creates the user and stores it inside the unit of work, so the
// writes that belong to the registration share one transaction. Later writes,
// such as the root folder, join the same callback.
func (uc *UseCase) Register(
	ctx context.Context,
	username, email, password string,
) (entity.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return entity.User{}, entity.ErrPasswordTooLong
		}

		return entity.User{}, fmt.Errorf(
			"UserUseCase - Register - bcrypt.GenerateFromPassword: %w",
			err,
		)
	}

	now := time.Now().UTC()

	// Self-registration never grants a role: an account starts as a plain user,
	// and an admin is granted out of band.
	user := entity.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Role:         entity.UserRoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		storeErr := uc.repo.Store(ctx, &user)
		if storeErr != nil {
			return fmt.Errorf("uc.repo.Store: %w", storeErr)
		}

		return nil
	})
	if err != nil {
		return entity.User{}, fmt.Errorf("UserUseCase - Register - uc.uow.Do: %w", err)
	}

	return user, nil
}

// Login verifies the credentials and opens a session, returning the opaque
// session identifier the caller stores in its cookie and the account behind
// it, so the caller can render the page for that role.
func (uc *UseCase) Login(ctx context.Context, email, password string) (string, entity.User, error) {
	user, err := uc.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return "", entity.User{}, entity.ErrInvalidCredentials
		}

		return "", entity.User{}, fmt.Errorf("UserUseCase - Login - uc.repo.GetByEmail: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", entity.User{}, entity.ErrInvalidCredentials
		}

		return "", entity.User{}, fmt.Errorf("UserUseCase - Login - bcrypt.CompareHashAndPassword: %w", err)
	}

	sessionID, err := newSessionID()
	if err != nil {
		return "", entity.User{}, fmt.Errorf("UserUseCase - Login - newSessionID: %w", err)
	}

	err = uc.sessions.Store(ctx, sessionID, entity.Session{UserID: user.ID, Role: user.Role}, uc.sessionTTL)
	if err != nil {
		return "", entity.User{}, fmt.Errorf("UserUseCase - Login - uc.sessions.Store: %w", err)
	}

	return sessionID, user, nil
}

// Authenticate resolves a session identifier to the session behind it. An
// unknown or expired session returns ErrSessionNotFound via the repository.
func (uc *UseCase) Authenticate(ctx context.Context, sessionID string) (entity.Session, error) {
	session, err := uc.sessions.Get(ctx, sessionID)
	if err != nil {
		return entity.Session{}, fmt.Errorf("UserUseCase - Authenticate - uc.sessions.Get: %w", err)
	}

	return session, nil
}

// Logout ends a session.
func (uc *UseCase) Logout(ctx context.Context, sessionID string) error {
	err := uc.sessions.Delete(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("UserUseCase - Logout - uc.sessions.Delete: %w", err)
	}

	return nil
}

// GetUser -.
func (uc *UseCase) GetUser(ctx context.Context, userID string) (entity.User, error) {
	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		return entity.User{}, fmt.Errorf("UserUseCase - GetUser - uc.repo.GetByID: %w", err)
	}

	return user, nil
}

// ListUsers returns one page of accounts as the read model a list renders,
// plus how many accounts match the filter in total. The filter is clamped to
// the bounds the list serves before it reaches the repository.
func (uc *UseCase) ListUsers(ctx context.Context, filter entity.UserFilter) (entity.UserList, error) {
	filter.Sanitize()

	users, err := uc.repo.List(ctx, filter)
	if err != nil {
		return entity.UserList{}, fmt.Errorf("UserUseCase - ListUsers - uc.repo.List: %w", err)
	}

	return users, nil
}

// SetRole changes the role of an account and ends its live sessions. The role
// travels with the session, so the sessions have to go: the account picks the
// new role up on its next sign-in, and nobody keeps a role that was taken away.
func (uc *UseCase) SetRole(ctx context.Context, email string, role entity.UserRole) error {
	if !role.IsValid() {
		return entity.ErrInvalidRole
	}

	user, err := uc.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.ErrUserNotFound
		}

		return fmt.Errorf("UserUseCase - SetRole - uc.repo.GetByEmail: %w", err)
	}

	err = uc.repo.UpdateRole(ctx, user.ID, role)
	if err != nil {
		return fmt.Errorf("UserUseCase - SetRole - uc.repo.UpdateRole: %w", err)
	}

	err = uc.sessions.DeleteUserSessions(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("UserUseCase - SetRole - uc.sessions.DeleteUserSessions: %w", err)
	}

	return nil
}

// UpdateUser rewrites an account: its name, its email, its role and, when the
// caller brings one, its password. It reports whether the live sessions were
// ended, which only happens when the role or the password changed, because
// those are the values a session holds a snapshot of; a rename never signs
// anybody out.
func (uc *UseCase) UpdateUser(
	ctx context.Context,
	userID, username, email string,
	role entity.UserRole,
	password string,
) (entity.User, bool, error) {
	if !role.IsValid() {
		return entity.User{}, false, entity.ErrInvalidRole
	}

	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.User{}, false, entity.ErrUserNotFound
		}

		return entity.User{}, false, fmt.Errorf("UserUseCase - UpdateUser - uc.repo.GetByID: %w", err)
	}

	roleChanged := user.Role != role

	passwordChanged := password != ""

	if passwordChanged {
		hash, hashErr := newPasswordHash(password)
		if hashErr != nil {
			return entity.User{}, false, hashErr
		}

		user.PasswordHash = hash
	}

	user.Username = username
	user.Email = email
	user.Role = role
	user.UpdatedAt = time.Now().UTC()

	err = uc.repo.Update(ctx, &user)
	if err != nil {
		return entity.User{}, false, fmt.Errorf("UserUseCase - UpdateUser - uc.repo.Update: %w", err)
	}

	if !passwordChanged && !roleChanged {
		return user, false, nil
	}

	err = uc.sessions.DeleteUserSessions(ctx, user.ID)
	if err != nil {
		return entity.User{}, false, fmt.Errorf(
			"UserUseCase - UpdateUser - uc.sessions.DeleteUserSessions: %w",
			err,
		)
	}

	return user, true, nil
}

// newPasswordHash hashes the new password an update brought in. An
// over-limit password is a caller error, not a server fault, so it maps to
// the sentinel instead of a wrapped error.
func newPasswordHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return "", entity.ErrPasswordTooLong
		}

		return "", fmt.Errorf("UserUseCase - newPasswordHash - bcrypt.GenerateFromPassword: %w", err)
	}

	return string(hash), nil
}

// newSessionID returns a cryptographically random, URL-safe session identifier.
func newSessionID() (string, error) {
	buf := make([]byte, _sessionIDBytes)

	_, err := rand.Read(buf)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - newSessionID - rand.Read: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
