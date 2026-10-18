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
	sessionTTL time.Duration
}

// New returns a User usecase.
func New(r repo.UserRepo, s repo.SessionRepo, sessionTTL time.Duration) usecase.User {
	return &UseCase{
		repo:       r,
		sessions:   s,
		sessionTTL: sessionTTL,
	}
}

// Register -.
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

	user := entity.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err = uc.repo.Store(ctx, &user)
	if err != nil {
		return entity.User{}, fmt.Errorf("UserUseCase - Register - uc.repo.Store: %w", err)
	}

	return user, nil
}

// Login verifies the credentials and opens a session, returning the opaque
// session identifier the caller stores in its cookie.
func (uc *UseCase) Login(ctx context.Context, email, password string) (string, error) {
	user, err := uc.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return "", entity.ErrInvalidCredentials
		}

		return "", fmt.Errorf("UserUseCase - Login - uc.repo.GetByEmail: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", entity.ErrInvalidCredentials
		}

		return "", fmt.Errorf("UserUseCase - Login - bcrypt.CompareHashAndPassword: %w", err)
	}

	sessionID, err := newSessionID()
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - newSessionID: %w", err)
	}

	err = uc.sessions.Store(ctx, sessionID, user.ID, uc.sessionTTL)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - uc.sessions.Store: %w", err)
	}

	return sessionID, nil
}

// Authenticate resolves a session identifier to its user ID. An unknown or
// expired session returns ErrSessionNotFound via the repository.
func (uc *UseCase) Authenticate(ctx context.Context, sessionID string) (string, error) {
	userID, err := uc.sessions.GetUserID(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Authenticate - uc.sessions.GetUserID: %w", err)
	}

	return userID, nil
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

// newSessionID returns a cryptographically random, URL-safe session identifier.
func newSessionID() (string, error) {
	buf := make([]byte, _sessionIDBytes)

	_, err := rand.Read(buf)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - newSessionID - rand.Read: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
