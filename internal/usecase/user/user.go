package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evrone/go-clean-template/internal/entity"
	"github.com/evrone/go-clean-template/internal/repo"
	"github.com/evrone/go-clean-template/internal/usecase"
	"github.com/evrone/go-clean-template/pkg/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// UseCase -.
type UseCase struct {
	repo repo.UserRepo
	jwt  *jwt.Manager
}

// New returns a User usecase.
func New(r repo.UserRepo, j *jwt.Manager) usecase.User {
	return &UseCase{
		repo: r,
		jwt:  j,
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

// Login -.
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

	token, err := uc.jwt.GenerateToken(user.ID)
	if err != nil {
		return "", fmt.Errorf("UserUseCase - Login - uc.jwt.GenerateToken: %w", err)
	}

	return token, nil
}

// GetUser -.
func (uc *UseCase) GetUser(ctx context.Context, userID string) (entity.User, error) {
	user, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		return entity.User{}, fmt.Errorf("UserUseCase - GetUser - uc.repo.GetByID: %w", err)
	}

	return user, nil
}
