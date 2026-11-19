// Package transfer implements the per-account transfer pacer. It hands every
// account one bucket per direction in Redis, so the rate stays the same across
// the requests of one process instead of restarting with each part.
package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// _rateSuffix namespaces the bucket of one account and one direction.
	_rateSuffix = "-rate:"

	// _rateTTL drops a bucket an hour after its last write, so an account
	// that stops sending does not leave a key behind.
	_rateTTL = time.Hour

	// _reserveScript reads the next-send time, moves it forward by the bytes
	// of this reservation, and stores it in one atomic Redis step. A plain
	// get-then-set would let two processes reserve the same slot.
	_reserveScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local bytes = tonumber(ARGV[2])
local rate = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local current = redis.call('GET', key)
local nextSend = 0
if current then
  nextSend = tonumber(current)
end
local sendAt = nextSend
if now > sendAt then
  sendAt = now
end
local newNext = sendAt + math.floor(bytes * 1000000 / rate)
redis.call('SET', key, newNext, 'PX', ttl)
return sendAt
`
)

// Repo is the Redis-backed TransferLimiter. The reservation is one Lua script,
// so the read-modify-write of the stored next-send time stays atomic across
// every process and instance sharing the bucket.
type Repo struct {
	rdb  *redis.Redis
	now  func() time.Time
	wait func(context.Context, time.Duration) error
}

// New returns a TransferLimiter that reserves its buckets in Redis.
func New(rdb *redis.Redis) repo.TransferLimiter {
	return &Repo{
		rdb:  rdb,
		now:  time.Now,
		wait: wait,
	}
}

// Pace reserves bytes for one account and direction at rate bytes per second
// and returns when the reservation may send. An account with no rate, or a
// send of nothing, is not paced and never touches Redis.
func (r *Repo) Pace(
	ctx context.Context,
	userID string,
	scope repo.TransferScope,
	bytes, rate int64,
) error {
	if rate <= 0 || bytes <= 0 {
		return nil
	}

	sendAt, err := r.reserve(ctx, userID, scope, bytes, rate)
	if err != nil {
		return err
	}

	// The reservation is already stored, so the next caller can reserve while
	// this one sleeps instead of queueing behind the delay.
	err = r.wait(ctx, sendAt.Sub(r.now()))
	if err != nil {
		return fmt.Errorf("transfer - Pace - r.wait: %w", err)
	}

	return nil
}

// reserve moves the next-send time of the account forward by the time bytes
// take at rate and returns the moment the caller may send. The Lua script
// covers the read and the write together, so two callers cannot reserve the
// same slot even when they run in different processes.
func (r *Repo) reserve(
	ctx context.Context,
	userID string,
	scope repo.TransferScope,
	bytes, rate int64,
) (time.Time, error) {
	key := rateKey(scope, userID)
	nowMicros := r.now().UnixMicro()

	sendAt, err := goredis.NewScript(_reserveScript).Run(
		ctx,
		r.rdb.Client,
		[]string{key},
		nowMicros,
		bytes,
		rate,
		_rateTTL.Milliseconds(),
	).Int64()
	if err != nil {
		return time.Time{}, fmt.Errorf("transfer - Pace - r.rdb.Client.Script.Run: %w", err)
	}

	return time.UnixMicro(sendAt), nil
}

// wait sleeps for d unless the request ends first, so a canceled upload stops
// pacing instead of holding the handler.
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// rateKey builds the Redis key for one direction of an account's budget.
func rateKey(scope repo.TransferScope, userID string) string {
	return string(scope) + _rateSuffix + userID
}
