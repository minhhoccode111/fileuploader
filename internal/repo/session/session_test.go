package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
)

const _testTTL = time.Hour

// newTestRepo starts an in-memory Redis and returns the repository under test
// together with the client, so a test can look at the keys it leaves behind.
func newTestRepo(t *testing.T) (repo.SessionRepo, *redis.Redis) {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb, err := redis.New(mr.Addr())
	if err != nil {
		t.Fatalf("redis.New() error = %v, want nil", err)
	}

	t.Cleanup(rdb.Close)

	return New(rdb), rdb
}

// storeSession writes one session for an account.
func storeSession(t *testing.T, sessions repo.SessionRepo, id string, session entity.Session) {
	t.Helper()

	if err := sessions.Store(context.Background(), id, session, _testTTL); err != nil {
		t.Fatalf("Store(%q) error = %v, want nil", id, err)
	}
}

// A password change keeps the session that asked for it and ends the rest, so
// the browser on screen stays signed in while the other devices do not.
func TestDeleteUserSessionsExceptKeepsTheNamedSession(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)

	ctx := t.Context()
	keep := entity.Session{UserID: "user", Role: entity.UserRoleUser}
	drop := entity.Session{UserID: "user", Role: entity.UserRoleAdmin}
	other := entity.Session{UserID: "other", Role: entity.UserRoleUser}

	storeSession(t, sessions, "keep", keep)
	storeSession(t, sessions, "drop", drop)
	storeSession(t, sessions, "other", other)

	err := sessions.DeleteUserSessionsExcept(ctx, "user", "keep")
	if err != nil {
		t.Fatalf("DeleteUserSessionsExcept() error = %v, want nil", err)
	}

	if got, err := sessions.Get(ctx, "keep"); err != nil || got.UserID != "user" {
		t.Errorf("Get(keep) = %+v, %v, want the kept session", got, err)
	}

	if _, err := sessions.Get(ctx, "drop"); !errors.Is(err, entity.ErrSessionNotFound) {
		t.Errorf("Get(drop) error = %v, want %v", err, entity.ErrSessionNotFound)
	}

	if got, err := sessions.Get(ctx, "other"); err != nil || got.UserID != "other" {
		t.Errorf("Get(other) = %+v, %v, want the session of another account", got, err)
	}

	members, err := rdb.Client.SMembers(ctx, userKey("user")).Result()
	if err != nil {
		t.Fatalf("SMembers() error = %v, want nil", err)
	}

	if len(members) != 1 || members[0] != "keep" {
		t.Errorf("live sessions = %v, want [keep]", members)
	}
}

// An empty keep identifier ends every session of the account, which is what a
// caller that lost track of its own session asks for.
func TestDeleteUserSessionsExceptAnEmptyKeepEndsEverySession(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)

	ctx := t.Context()
	session := entity.Session{UserID: "user", Role: entity.UserRoleUser}

	storeSession(t, sessions, "one", session)
	storeSession(t, sessions, "two", session)

	err := sessions.DeleteUserSessionsExcept(ctx, "user", "")
	if err != nil {
		t.Fatalf("DeleteUserSessionsExcept() error = %v, want nil", err)
	}

	for _, id := range []string{"one", "two"} {
		if _, err := sessions.Get(ctx, id); !errors.Is(err, entity.ErrSessionNotFound) {
			t.Errorf("Get(%q) error = %v, want %v", id, err, entity.ErrSessionNotFound)
		}
	}

	if members, err := rdb.Client.SMembers(ctx, userKey("user")).Result(); err != nil || len(members) != 0 {
		t.Errorf("live sessions = %v, %v, want none", members, err)
	}
}
