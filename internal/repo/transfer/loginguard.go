package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// _reserveLoginScript takes one attempt slot and opens the window on the first
// one. The count and the expiry are one atomic step, so concurrent attempts
// cannot all read the same count and pass together.
const _reserveLoginScript = `
local count = redis.call("incr", KEYS[1])
if count == 1 then
	redis.call("pexpire", KEYS[1], ARGV[1])
end
return count
`

// LoginGuard is the Redis-backed sign-in attempt counter. One key per bucket,
// so a stored count never outlives the window it covers.
type LoginGuard struct {
	rdb *redis.Redis
}

// NewLoginGuard returns a LoginGuard that keeps its counters in Redis.
func NewLoginGuard(rdb *redis.Redis) repo.LoginGuard {
	return &LoginGuard{rdb: rdb}
}

// Reserve takes one attempt slot from the bucket and reports whether the
// attempt may go on. A limit or window of zero disables the check. The slot is
// taken even when the answer is no, so a blocked attempt cannot shrink the
// window for the next one.
func (g *LoginGuard) Reserve(
	ctx context.Context,
	scope repo.LoginScope,
	id string,
	limit int,
	window time.Duration,
) (bool, error) {
	if limit <= 0 || window <= 0 {
		return true, nil
	}

	count, err := goredis.NewScript(_reserveLoginScript).Run(
		ctx,
		g.rdb.Client,
		[]string{loginKey(scope, id)},
		window.Milliseconds(),
	).Int64()
	if err != nil {
		return false, fmt.Errorf("transfer - LoginGuard.Reserve - script run: %w", err)
	}

	return count <= int64(limit), nil
}

// Reset clears the bucket, which a successful sign-in does for the email it
// signed in, so a correct password ends the count it earned.
func (g *LoginGuard) Reset(ctx context.Context, scope repo.LoginScope, id string) error {
	err := g.rdb.Client.Del(ctx, loginKey(scope, id)).Err()
	if err != nil {
		return fmt.Errorf("transfer - LoginGuard.Reset - r.rdb.Client.Del: %w", err)
	}

	return nil
}

// loginKey builds the Redis key of one bucket.
func loginKey(scope repo.LoginScope, id string) string {
	return string(scope) + id
}
