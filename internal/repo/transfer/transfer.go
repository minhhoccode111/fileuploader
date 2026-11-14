// Package transfer implements the per-account upload pacer. It hands every
// account a bucket held in Redis, so the rate stays the same across the
// requests of one process instead of restarting with each part.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// _ratePrefix namespaces the bucket of one account.
	_ratePrefix = "upload-rate:"

	// _microsPerSecond turns a byte count into the microseconds the bucket
	// must hold the account back for.
	_microsPerSecond = 1_000_000

	// _rateTTL drops a bucket an hour after its last write, so an account
	// that stops sending does not leave a key behind.
	_rateTTL = time.Hour
)

// Repo is the Redis-backed TransferLimiter. The mutex makes the
// read-modify-write of the stored next-send time atomic within the process,
// which is enough for one app instance; separate instances pace apart.
type Repo struct {
	rdb  *redis.Redis
	mu   sync.Mutex
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

// Pace reserves bytes for one account at rate bytes per second and returns
// when the reservation may send. An account with no rate, or a send of
// nothing, is not paced and never touches Redis.
func (r *Repo) Pace(ctx context.Context, userID string, bytes, rate int64) error {
	if rate <= 0 || bytes <= 0 {
		return nil
	}

	sendAt, err := r.reserve(ctx, userID, bytes, rate)
	if err != nil {
		return err
	}

	// The wait happens outside the mutex, so the next caller can reserve
	// while this one sleeps instead of queueing behind the delay.
	err = r.wait(ctx, sendAt.Sub(r.now()))
	if err != nil {
		return fmt.Errorf("transfer - Pace - r.wait: %w", err)
	}

	return nil
}

// reserve moves the next-send time of the account forward by the time bytes
// take at rate and returns the moment the caller may send. The mutex covers
// the read and the write together, so two callers cannot reserve the same
// slot.
func (r *Repo) reserve(ctx context.Context, userID string, bytes, rate int64) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := rateKey(userID)

	nowMicros := r.now().UnixMicro()

	next, err := r.rdb.Client.Get(ctx, key).Int64()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return time.Time{}, fmt.Errorf("transfer - Pace - r.rdb.Client.Get: %w", err)
	}

	sendAt := max(next, nowMicros)
	newNext := sendAt + bytes*_microsPerSecond/rate

	err = r.rdb.Client.Set(ctx, key, newNext, _rateTTL).Err()
	if err != nil {
		return time.Time{}, fmt.Errorf("transfer - Pace - r.rdb.Client.Set: %w", err)
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

// rateKey builds the Redis key for the bucket of an account.
func rateKey(userID string) string {
	return _ratePrefix + userID
}
