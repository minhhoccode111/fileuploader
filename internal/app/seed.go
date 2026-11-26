package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// _seedTargetCap is the number of accounts the environment can name.
const _seedTargetCap = 2

// seedTarget is one account the environment asks the first run to create.
type seedTarget struct {
	username string
	email    string
	password string
	role     entity.UserRole
	status   entity.UserStatus
}

// seedTargets lists the accounts the config names. An account appears only
// when every one of its three variables is set, so a half-configured seed is
// skipped instead of creating an account nobody can sign in with.
func seedTargets(cfg *config.Config) []seedTarget {
	targets := make([]seedTarget, 0, _seedTargetCap)

	if cfg.Seed.AdminEmail != "" && cfg.Seed.AdminUsername != "" && cfg.Seed.AdminPassword != "" {
		targets = append(targets, seedTarget{
			username: cfg.Seed.AdminUsername,
			email:    cfg.Seed.AdminEmail,
			password: cfg.Seed.AdminPassword,
			role:     entity.UserRoleAdmin,
			status:   entity.UserStatusActive,
		})
	}

	if cfg.Seed.DemoEmail != "" && cfg.Seed.DemoUsername != "" && cfg.Seed.DemoPassword != "" {
		targets = append(targets, seedTarget{
			username: cfg.Seed.DemoUsername,
			email:    cfg.Seed.DemoEmail,
			password: cfg.Seed.DemoPassword,
			role:     entity.UserRoleUser,
			status:   entity.UserStatusActive,
		})
	}

	return targets
}

// seedUsers creates the accounts the environment names. It runs at every
// startup, not only the first one: an account whose email or username is
// taken is skipped, so a restart is a no-op and a password change in the
// environment never rewrites an existing account.
//
// A seed that fails for another reason is logged and does not stop the app:
// the site serves regardless, and the operator reads the line.
func seedUsers(ctx context.Context, users usecase.User, cfg *config.Config, l logger.Interface) {
	for _, target := range seedTargets(cfg) {
		_, err := users.SeedUser(
			ctx, target.username, target.email, target.password, target.role, target.status,
		)

		switch {
		case err == nil:
			l.Info("app - seedUsers - seeded %s", target.email)
		case errors.Is(err, entity.ErrEmailAlreadyExists),
			errors.Is(err, entity.ErrUsernameAlreadyExists):
			l.Info("app - seedUsers - %s already exists, skipped", target.email)
		default:
			l.Error(fmt.Errorf("app - seedUsers - %s: %w", target.email, err))
		}
	}
}
