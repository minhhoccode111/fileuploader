// Package session implements the Redis-backed Session repository.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Key prefixes. The first namespaces the sessions themselves, the second the
// per-user indexes that list the live sessions of one account, so a role
// change can end them all.
const (
	_keyPrefix     = "session:"
	_userKeyPrefix = "user-sessions:"
)

// Repo -.
type Repo struct {
	rdb *redis.Redis
}

// New returns a Session repository.
func New(rdb *redis.Redis) repo.SessionRepo {
	return &Repo{rdb: rdb}
}

// Store saves a session that expires after ttl. The session carries the role
// of the account, and the account's index of live sessions is kept next to it,
// so a role change can end every session that account holds.
func (r *Repo) Store(ctx context.Context, id string, session entity.Session, ttl time.Duration) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("SessionRepo - Store - json.Marshal: %w", err)
	}

	pipe := r.rdb.Client.TxPipeline()
	pipe.Set(ctx, key(id), payload, ttl)
	pipe.SAdd(ctx, userKey(session.UserID), id)
	pipe.Expire(ctx, userKey(session.UserID), ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("SessionRepo - Store - pipe.Exec: %w", err)
	}

	return nil
}

// Get returns the session behind a live identifier. A missing or expired key
// maps to ErrSessionNotFound.
func (r *Repo) Get(ctx context.Context, id string) (entity.Session, error) {
	payload, err := r.rdb.Client.Get(ctx, key(id)).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return entity.Session{}, entity.ErrSessionNotFound
		}

		return entity.Session{}, fmt.Errorf("SessionRepo - Get - r.rdb.Client.Get: %w", err)
	}

	var session entity.Session

	err = json.Unmarshal(payload, &session)
	if err != nil {
		return entity.Session{}, fmt.Errorf("SessionRepo - Get - json.Unmarshal: %w", err)
	}

	return session, nil
}

// Delete ends a session and drops it from the index of its account.
func (r *Repo) Delete(ctx context.Context, id string) error {
	session, err := r.Get(ctx, id)
	if err != nil {
		if errors.Is(err, entity.ErrSessionNotFound) {
			return nil
		}

		return fmt.Errorf("SessionRepo - Delete - r.Get: %w", err)
	}

	pipe := r.rdb.Client.TxPipeline()
	pipe.Del(ctx, key(id))
	pipe.SRem(ctx, userKey(session.UserID), id)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("SessionRepo - Delete - pipe.Exec: %w", err)
	}

	return nil
}

// DeleteUserSessions ends every live session of an account. A role change
// calls it, so the account picks up its new role on the next sign-in instead
// of keeping the role its session was created with.
func (r *Repo) DeleteUserSessions(ctx context.Context, userID string) error {
	return r.DeleteUserSessionsExcept(ctx, userID, "")
}

// DeleteUserSessionsExcept ends every live session of an account but the one
// named, so the caller that asked for a password change stays signed in. An
// empty identifier ends every session, which is what a caller without a
// session of its own asks for.
func (r *Repo) DeleteUserSessionsExcept(
	ctx context.Context,
	userID, keepSessionID string,
) error {
	index := userKey(userID)

	ids, err := r.rdb.Client.SMembers(ctx, index).Result()
	if err != nil {
		return fmt.Errorf("SessionRepo - DeleteUserSessionsExcept - r.rdb.Client.SMembers: %w", err)
	}

	stale := make([]any, 0, len(ids))

	keys := make([]string, 0, len(ids))

	for _, id := range ids {
		if id == keepSessionID {
			continue
		}

		stale = append(stale, id)

		keys = append(keys, key(id))
	}

	if len(stale) == 0 {
		return nil
	}

	pipe := r.rdb.Client.TxPipeline()
	pipe.Del(ctx, keys...)
	pipe.SRem(ctx, index, stale...)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("SessionRepo - DeleteUserSessionsExcept - pipe.Exec: %w", err)
	}

	return nil
}

// key builds the Redis key for a session identifier.
func key(id string) string {
	return _keyPrefix + id
}

// userKey builds the Redis key for the index of the sessions of an account.
func userKey(userID string) string {
	return _userKeyPrefix + userID
}
