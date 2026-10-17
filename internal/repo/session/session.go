// Package session implements the Redis-backed Session repository.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evrone/go-clean-template/internal/entity"
	"github.com/evrone/go-clean-template/internal/repo"
	"github.com/evrone/go-clean-template/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// _keyPrefix namespaces session keys so they cannot collide with other data.
const _keyPrefix = "session:"

// Repo -.
type Repo struct {
	rdb *redis.Redis
}

// New returns a Session repository.
func New(rdb *redis.Redis) repo.SessionRepo {
	return &Repo{rdb: rdb}
}

// Store saves a session that expires after ttl. Redis enforces the expiry, so
// there is nothing to clean up.
func (r *Repo) Store(ctx context.Context, id, userID string, ttl time.Duration) error {
	err := r.rdb.Client.Set(ctx, key(id), userID, ttl).Err()
	if err != nil {
		return fmt.Errorf("SessionRepo - Store - rdb.Client.Set: %w", err)
	}

	return nil
}

// GetUserID returns the user ID for a live session. A missing or expired key
// maps to ErrSessionNotFound.
func (r *Repo) GetUserID(ctx context.Context, id string) (string, error) {
	userID, err := r.rdb.Client.Get(ctx, key(id)).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", entity.ErrSessionNotFound
		}

		return "", fmt.Errorf("SessionRepo - GetUserID - rdb.Client.Get: %w", err)
	}

	return userID, nil
}

// Delete ends a session.
func (r *Repo) Delete(ctx context.Context, id string) error {
	err := r.rdb.Client.Del(ctx, key(id)).Err()
	if err != nil {
		return fmt.Errorf("SessionRepo - Delete - rdb.Client.Del: %w", err)
	}

	return nil
}

// key builds the Redis key for a session identifier.
func key(id string) string {
	return _keyPrefix + id
}
