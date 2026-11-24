package transfer

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
)

const (
	// _testLoginLimit is small enough to reach in a few calls.
	_testLoginLimit = 3

	// _testLoginWindow is the window every counter in these tests covers.
	_testLoginWindow = 15 * time.Minute
)

// newLoginTestGuard starts an in-memory Redis and returns the guard under test
// with the store behind it.
func newLoginTestGuard(t *testing.T) (repo.LoginGuard, *redis.Redis, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb, err := redis.New(mr.Addr())
	if err != nil {
		t.Fatalf("redis.New() error = %v, want nil", err)
	}

	t.Cleanup(rdb.Close)

	return NewLoginGuard(rdb), rdb, mr
}

// reserveOne takes one slot from a bucket and returns whether it was granted.
func reserveOne(t *testing.T, guard repo.LoginGuard, scope repo.LoginScope, id string) bool {
	t.Helper()

	allowed, err := guard.Reserve(
		context.Background(), scope, id, _testLoginLimit, _testLoginWindow,
	)
	if err != nil {
		t.Fatalf("Reserve() error = %v, want nil", err)
	}

	return allowed
}

func TestLoginGuardAllowsAnUntouchedBucket(t *testing.T) {
	t.Parallel()

	guard, _, _ := newLoginTestGuard(t)

	if !reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = false, want true for an untouched bucket")
	}
}

func TestLoginGuardAllowsTheLastSlot(t *testing.T) {
	t.Parallel()

	guard, _, _ := newLoginTestGuard(t)

	for range _testLoginLimit - 1 {
		reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com")
	}

	if !reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = false, want true for the last slot")
	}
}

func TestLoginGuardRefusesTheAttemptOverTheLimit(t *testing.T) {
	t.Parallel()

	guard, _, _ := newLoginTestGuard(t)

	for range _testLoginLimit {
		reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com")
	}

	if reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = true, want false over the limit")
	}
}

func TestLoginGuardKeepsTheScopesApart(t *testing.T) {
	t.Parallel()

	guard, _, _ := newLoginTestGuard(t)

	for range _testLoginLimit {
		reserveOne(t, guard, repo.LoginScopeIP, "10.0.0.1")
	}

	if !reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = false, want true: an IP bucket must not block an email bucket")
	}
}

func TestLoginGuardResetClearsTheBucket(t *testing.T) {
	t.Parallel()

	guard, _, _ := newLoginTestGuard(t)

	for range _testLoginLimit {
		reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com")
	}

	err := guard.Reset(context.Background(), repo.LoginScopeEmail, "john@example.com")
	if err != nil {
		t.Fatalf("Reset() error = %v, want nil", err)
	}

	if !reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = false, want true after Reset()")
	}
}

func TestLoginGuardCounterExpiresWithTheWindow(t *testing.T) {
	t.Parallel()

	guard, rdb, mr := newLoginTestGuard(t)
	reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com")

	key := loginKey(repo.LoginScopeEmail, "john@example.com")

	ttl := rdb.Client.TTL(context.Background(), key).Val()
	if ttl <= 0 || ttl > _testLoginWindow {
		t.Fatalf("TTL() = %v, want a value in (0, %v]", ttl, _testLoginWindow)
	}

	mr.FastForward(_testLoginWindow + time.Second)

	if !reserveOne(t, guard, repo.LoginScopeEmail, "john@example.com") {
		t.Error("Reserve() = false, want true after the window expired")
	}
}
