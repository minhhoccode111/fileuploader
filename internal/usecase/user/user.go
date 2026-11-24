package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"golang.org/x/crypto/bcrypt"
)

const (
	// _sessionIDBytes is the size of the random session identifier. 32 bytes
	// (256 bits) makes guessing a live session infeasible.
	_sessionIDBytes = 32

	// _loginEmailLimit is how many failed sign-ins one email address may make
	// per _loginWindow before it is refused without checking the password.
	_loginEmailLimit = 5

	// _loginIPLimit is how many failed sign-ins one client address may make per
	// _loginWindow, so one address cannot spray many accounts.
	_loginIPLimit = 30

	// _loginWindow is the period a failed sign-in counts against. It is short
	// enough that a mistyping visitor recovers on their own.
	_loginWindow = 15 * time.Minute
)

// UseCase -.
type UseCase struct {
	repo       repo.UserRepo
	sessions   repo.SessionRepo
	folders    repo.FolderRepo
	uow        repo.UnitOfWork
	guard      repo.LoginGuard
	sessionTTL time.Duration
}

// New returns a User usecase.
func New(
	r repo.UserRepo,
	s repo.SessionRepo,
	f repo.FolderRepo,
	u repo.UnitOfWork,
	guard repo.LoginGuard,
	sessionTTL time.Duration,
) usecase.User {
	return &UseCase{
		repo:       r,
		sessions:   s,
		folders:    f,
		uow:        u,
		guard:      guard,
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
		Status:       entity.UserStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	root := entity.Folder{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Name:      entity.RootFolderName,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		storeErr := uc.repo.Store(ctx, &user)
		if storeErr != nil {
			return fmt.Errorf("uc.repo.Store: %w", storeErr)
		}

		rootErr := uc.folders.Store(ctx, &root)
		if rootErr != nil {
			return fmt.Errorf("uc.folders.Store: %w", rootErr)
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
//
// Every attempt takes one slot from the email bucket and the client bucket
// before the password is checked. The slot is taken atomically, so a burst of
// concurrent guesses cannot all pass on the same count. Once either bucket is
// full, the password is not checked at all, which caps both a guessed password
// and a password spray. A successful sign-in clears the email bucket but not
// the client one, so one working account cannot reset the budget of the
// address behind it.
func (uc *UseCase) Login(
	ctx context.Context,
	email, password, clientKey string,
) (string, entity.User, error) {
	emailKey := strings.ToLower(strings.TrimSpace(email))

	err := uc.reserveLoginAttempts(ctx, emailKey, clientKey)
	if err != nil {
		return "", entity.User{}, err
	}

	user, err := uc.verifyCredentials(ctx, emailKey, password)
	if err != nil {
		return "", entity.User{}, err
	}

	sessionID, err := uc.openSession(ctx, &user, emailKey)
	if err != nil {
		return "", entity.User{}, err
	}

	return sessionID, user, nil
}

// verifyCredentials checks the password of an account that passed the throttle.
// Every rejection reports the same sentinel, so a caller cannot tell an unknown
// email from a wrong password.
func (uc *UseCase) verifyCredentials(
	ctx context.Context,
	emailKey, password string,
) (entity.User, error) {
	user, err := uc.repo.GetByEmail(ctx, emailKey)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.User{}, entity.ErrInvalidCredentials
		}

		return entity.User{}, fmt.Errorf("UserUseCase - Login - uc.repo.GetByEmail: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return entity.User{}, entity.ErrInvalidCredentials
		}

		return entity.User{}, fmt.Errorf("UserUseCase - Login - bcrypt.CompareHashAndPassword: %w", err)
	}

	return user, nil
}

// openSession checks that the account may sign in and stores the session. The
// email bucket is cleared only after the password is known to be correct, so a
// failed attempt cannot free the slot it just took.
func (uc *UseCase) openSession(
	ctx context.Context,
	user *entity.User,
	emailKey string,
) (string, error) {
	if user.Status == entity.UserStatusPending {
		return "", entity.ErrUserPending
	}

	if !user.Status.CanSignIn() {
		return "", entity.ErrUserDisabled
	}

	err := uc.guard.Reset(ctx, repo.LoginScopeEmail, emailKey)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - uc.guard.Reset: %w", err)
	}

	sessionID, err := newSessionID()
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - newSessionID: %w", err)
	}

	err = uc.sessions.Store(ctx, sessionID, entity.Session{UserID: user.ID, Role: user.Role}, uc.sessionTTL)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - uc.sessions.Store: %w", err)
	}

	return sessionID, nil
}

// reserveLoginAttempts takes one slot from each bucket and refuses the sign-in
// once either is full. The email bucket is checked first, so the common case
// costs one store call. A store error fails closed: a throttle that fails open
// guards nothing, and the session store is the same Redis a sign-in needs
// anyway.
func (uc *UseCase) reserveLoginAttempts(ctx context.Context, emailKey, clientKey string) error {
	allowed, err := uc.guard.Reserve(
		ctx, repo.LoginScopeEmail, emailKey, _loginEmailLimit, _loginWindow,
	)
	if err != nil {
		return fmt.Errorf("UserUseCase - Login - uc.guard.Reserve(email): %w", err)
	}

	if !allowed {
		return entity.ErrLoginThrottled
	}

	if clientKey == "" {
		return nil
	}

	allowed, err = uc.guard.Reserve(
		ctx, repo.LoginScopeIP, clientKey, _loginIPLimit, _loginWindow,
	)
	if err != nil {
		return fmt.Errorf("UserUseCase - Login - uc.guard.Reserve(client): %w", err)
	}

	if !allowed {
		return entity.ErrLoginThrottled
	}

	return nil
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
func (uc *UseCase) ListUsers(ctx context.Context, filter *entity.UserFilter) (entity.UserList, error) {
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

// SetStatus changes the lifecycle state of an account. Disabling ends its live
// sessions, so a disabled account is signed out immediately.
func (uc *UseCase) SetStatus(ctx context.Context, userID string, status entity.UserStatus) error {
	if !status.IsValid() {
		return entity.ErrInvalidStatus
	}

	// End the sessions before the status changes: session deletion is
	// idempotent, and a logout without a status change is the safe failure
	// mode, while the reverse leaves a revoked account signed in.
	if !status.CanSignIn() {
		if err := uc.sessions.DeleteUserSessions(ctx, userID); err != nil {
			return fmt.Errorf("UserUseCase - SetStatus - uc.sessions.DeleteUserSessions: %w", err)
		}
	}

	err := uc.repo.UpdateStatus(ctx, userID, status)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.ErrUserNotFound
		}

		return fmt.Errorf("UserUseCase - SetStatus - uc.repo.UpdateStatus: %w", err)
	}

	return nil
}

// ApproveUser moves a pending account to active.
func (uc *UseCase) ApproveUser(ctx context.Context, userID string) error {
	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.ErrUserNotFound
		}

		return fmt.Errorf("UserUseCase - ApproveUser - uc.repo.GetByID: %w", err)
	}

	if user.Status != entity.UserStatusPending {
		return entity.ErrInvalidStatus
	}

	return uc.SetStatus(ctx, userID, entity.UserStatusActive)
}

// DisableUser moves an account to disabled and ends its sessions.
func (uc *UseCase) DisableUser(ctx context.Context, userID string) error {
	return uc.SetStatus(ctx, userID, entity.UserStatusDisabled)
}

// EnableUser moves a disabled account back to active.
func (uc *UseCase) EnableUser(ctx context.Context, userID string) error {
	return uc.SetStatus(ctx, userID, entity.UserStatusActive)
}

// UpdateStatus is an alias for SetStatus.
func (uc *UseCase) UpdateStatus(ctx context.Context, userID string, status entity.UserStatus) error {
	return uc.SetStatus(ctx, userID, status)
}

// SetUserLimits updates the per-account storage and transfer limits. Negative
// values are a caller error, not a server fault.
func (uc *UseCase) SetUserLimits(
	ctx context.Context,
	userID string,
	quota, uploadRate, downloadRate int64,
) (entity.User, error) {
	if quota < 0 || uploadRate < 0 || downloadRate < 0 {
		return entity.User{}, entity.ErrInvalidLimits
	}

	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.User{}, entity.ErrUserNotFound
		}

		return entity.User{}, fmt.Errorf("UserUseCase - SetUserLimits - uc.repo.GetByID: %w", err)
	}

	err = uc.repo.SetUserLimits(ctx, userID, quota, uploadRate, downloadRate)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.User{}, entity.ErrUserNotFound
		}

		return entity.User{}, fmt.Errorf("UserUseCase - SetUserLimits - uc.repo.SetUserLimits: %w", err)
	}

	user.StorageQuotaBytes = quota
	user.UploadRateBytesPerSec = uploadRate
	user.DownloadRateBytesPerSec = downloadRate
	user.UpdatedAt = time.Now().UTC()

	return user, nil
}

// UpdateProfile rewrites the name and the email of an account, which are the
// two fields its holder owns. The role, the status and the password stay as
// they are, so a profile save never changes what the account may do.
func (uc *UseCase) UpdateProfile(
	ctx context.Context,
	userID, username, email string,
) (entity.User, error) {
	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.User{}, entity.ErrUserNotFound
		}

		return entity.User{}, fmt.Errorf("UserUseCase - UpdateProfile - uc.repo.GetByID: %w", err)
	}

	updatedAt := time.Now().UTC()

	err = uc.repo.UpdateProfile(ctx, userID, username, email)
	if err != nil {
		return entity.User{}, fmt.Errorf("UserUseCase - UpdateProfile - uc.repo.UpdateProfile: %w", err)
	}

	user.Username = username
	user.Email = email
	user.UpdatedAt = updatedAt

	return user, nil
}

// ChangePassword replaces the password of an account after checking the one it
// holds now. A wrong current password is a rejected sign-in, not a server
// fault. The change ends every other session of the account and keeps the one
// that asked, so the browser on screen stays signed in while a stolen cookie
// on another device stops working.
func (uc *UseCase) ChangePassword(
	ctx context.Context,
	userID, currentPassword, newPassword, currentSessionID string,
) error {
	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return entity.ErrUserNotFound
		}

		return fmt.Errorf("UserUseCase - ChangePassword - uc.repo.GetByID: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return entity.ErrInvalidCredentials
		}

		return fmt.Errorf("UserUseCase - ChangePassword - bcrypt.CompareHashAndPassword: %w", err)
	}

	hash, err := newPasswordHash(newPassword)
	if err != nil {
		return err
	}

	// Revoke the other sessions before the hash is written. Revocation is
	// idempotent and safe to retry, while a hash that lands before a failed
	// revocation would leave a changed password with live sessions the
	// account meant to end. A hash write that fails afterwards leaves the old
	// password with the extra sessions gone, which is the safe direction.
	err = uc.sessions.DeleteUserSessionsExcept(ctx, userID, currentSessionID)
	if err != nil {
		return fmt.Errorf("UserUseCase - ChangePassword - uc.sessions.DeleteUserSessionsExcept: %w", err)
	}

	err = uc.repo.UpdatePassword(ctx, userID, hash)
	if err != nil {
		return fmt.Errorf("UserUseCase - ChangePassword - uc.repo.UpdatePassword: %w", err)
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
