package transfer

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
)

const _testNow = 1_760_000_000

// newTestRepo starts an in-memory Redis and returns the pacer under test with
// the waits its fake wait function recorded. The clock is frozen at now, so
// every duration is exact.
func newTestRepo(t *testing.T, now time.Time) (*Repo, *[]time.Duration) {
	t.Helper()

	r, waits, _ := newTestRepoWithServer(t, now)

	return r, waits
}

// newTestRepoWithServer is newTestRepo that also hands back the Redis server,
// so a test can close it and watch the pacer fail.
func newTestRepoWithServer(t *testing.T, now time.Time) (*Repo, *[]time.Duration, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb, err := redis.New(mr.Addr())
	if err != nil {
		t.Fatalf("redis.New() error = %v, want nil", err)
	}

	t.Cleanup(rdb.Close)

	var (
		mu    sync.Mutex
		waits []time.Duration
	)

	r := &Repo{
		rdb: rdb,
		now: func() time.Time { return now },
		wait: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			defer mu.Unlock()

			waits = append(waits, d)

			return nil
		},
	}

	return r, &waits, mr
}

// An account with no rate, or a send of nothing, is not paced and must not
// leave a bucket behind.
func TestPaceSkipsUnlimitedAndZeroBytes(t *testing.T) {
	t.Parallel()

	r, waits := newTestRepo(t, time.Unix(_testNow, 0))
	ctx := context.Background()

	err := r.Pace(ctx, "user", 1<<20, 0)
	if err != nil {
		t.Fatalf("Pace(rate=0) error = %v, want nil", err)
	}

	err = r.Pace(ctx, "user", 0, 1<<20)
	if err != nil {
		t.Fatalf("Pace(bytes=0) error = %v, want nil", err)
	}

	if len(*waits) != 0 {
		t.Errorf("waits = %v, want none", *waits)
	}

	exists, err := r.rdb.Client.Exists(ctx, "upload-rate:user").Result()
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}

	if exists != 0 {
		t.Errorf("upload-rate:user exists, want absent")
	}
}

// Two sends of one second worth of bytes each at one second per rate fill the
// same bucket: the first leaves at once, the second waits for its turn.
func TestPaceDelaysTheSecondReservation(t *testing.T) {
	t.Parallel()

	r, waits := newTestRepo(t, time.Unix(_testNow, 0))
	ctx := context.Background()

	for range 2 {
		err := r.Pace(ctx, "user", 1<<20, 1<<20)
		if err != nil {
			t.Fatalf("Pace() error = %v, want nil", err)
		}
	}

	want := []time.Duration{0, time.Second}
	if !slices.Equal(*waits, want) {
		t.Errorf("waits = %v, want %v", *waits, want)
	}
}

// Callers on the same account share one bucket, because the mutex serializes
// the read-modify-write of the stored next-send time.
func TestPaceSharesOneBucketAcrossCallers(t *testing.T) {
	t.Parallel()

	r, waits := newTestRepo(t, time.Unix(_testNow, 0))
	ctx := context.Background()

	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			err := r.Pace(ctx, "user", 1<<20, 1<<20)
			if err != nil {
				t.Errorf("Pace() error = %v, want nil", err)
			}
		})
	}

	wg.Wait()

	got := slices.Clone(*waits)
	slices.Sort(got)

	want := []time.Duration{0, time.Second}
	if !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

// A bucket the pacer cannot read or write is an error the caller must see, not
// a silent send without a rate.
func TestPaceReturnsRedisFailure(t *testing.T) {
	t.Parallel()

	r, _, mr := newTestRepoWithServer(t, time.Unix(_testNow, 0))
	mr.Close()

	err := r.Pace(context.Background(), "user", 1<<20, 1<<20)
	if err == nil {
		t.Fatal("Pace() error = nil, want a Redis failure")
	}

	if !strings.Contains(err.Error(), "transfer - Pace") {
		t.Errorf("Pace() error = %v, want it to carry the layer path", err)
	}
}
