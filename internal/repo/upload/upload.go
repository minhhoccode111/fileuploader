// Package upload implements the Redis-backed UploadSession repository.
package upload

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Key prefixes. The first two namespace a session and the parts it has
// received, the third the index of the sessions of one account, the fourth the
// sorted set that says which session is due for a sweep, and the last the
// per-account lock a start takes while it measures the quota.
const (
	_uploadPrefix    = "upload:"
	_partsPrefix     = "upload-parts:"
	_userPrefix      = "user-uploads:"
	_expiryKey       = "upload-expiry"
	_quotaLockPrefix = "upload-quota-lock:"
)

const (
	// _sessionGrace keeps a session hash alive a day past its expiry, so a
	// sweep that runs late still finds the session and can abort the upload.
	_sessionGrace = 24 * time.Hour

	// _lockTTL is how long the quota lock survives without a release, so a
	// process that dies mid-upload does not block the account forever. A live
	// holder renews it every _lockRenew.
	_lockTTL = 30 * time.Second

	// _lockRenew is how often the holder extends the lock while it works.
	_lockRenew = 10 * time.Second

	// _lockRetry is the pause between attempts to take the quota lock.
	_lockRetry = 25 * time.Millisecond
)

// _releaseScript removes the quota lock only when it still holds the token of
// the caller. A release that arrives after the lock expired and another caller
// took it leaves the new holder alone.
const _releaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`

// _renewScript extends the quota lock only when it still holds the token of
// the caller. A renewal that races a release or a new holder leaves the other
// caller's lock alone.
const _renewScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0
`

// Repo -.
type Repo struct {
	rdb *redis.Redis
}

// New returns an UploadSession repository.
func New(rdb *redis.Redis) repo.UploadSessionRepo {
	return &Repo{rdb: rdb}
}

// Create stores a session that expires after idleTTL. The session hash, the
// index of the account, and the expiry set are written in one round trip, so a
// start never leaves a session that no sweep can see.
//
//nolint:gocritic // the interface passes the session by value, so the copy is deliberate
func (r *Repo) Create(ctx context.Context, session entity.UploadSession, idleTTL time.Duration) error {
	fallback := idleTTL + _sessionGrace
	expiresAt := time.Now().Add(idleTTL)

	pipe := r.rdb.Client.TxPipeline()
	pipe.HSet(
		ctx,
		sessionKey(session.ID),
		"id", session.ID,
		"user_id", session.UserID,
		"folder_id", session.FolderID,
		"file_id", session.FileID,
		"name", session.Name,
		"content_type", session.ContentType,
		"size", strconv.FormatInt(session.Size, 10),
		"part_size", strconv.FormatInt(session.PartSize, 10),
		"part_count", strconv.Itoa(session.PartCount),
		"storage_key", session.StorageKey,
		"multipart_id", session.MultipartID,
		"created_at", strconv.FormatInt(session.CreatedAt.Unix(), 10),
		"expires_at", strconv.FormatInt(expiresAt.Unix(), 10),
	)
	pipe.SAdd(ctx, userKey(session.UserID), session.ID)
	pipe.ZAdd(ctx, _expiryKey, goredis.Z{Score: float64(expiresAt.Unix()), Member: session.ID})
	pipe.Expire(ctx, sessionKey(session.ID), fallback)
	pipe.Expire(ctx, partsKey(session.ID), fallback)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("upload - Create - pipe.Exec: %w", err)
	}

	return nil
}

// Get returns the session behind an identifier. A missing session maps to
// ErrUploadNotFound, so the caller answers the first part of an upload that
// never started with the same refusal as one that already expired.
func (r *Repo) Get(ctx context.Context, uploadID string) (entity.UploadSession, error) {
	fields, err := r.rdb.Client.HGetAll(ctx, sessionKey(uploadID)).Result()
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("upload - Get - r.rdb.Client.HGetAll: %w", err)
	}

	if len(fields) == 0 {
		return entity.UploadSession{}, entity.ErrUploadNotFound
	}

	session, err := sessionFromFields(uploadID, fields)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("upload - Get - sessionFromFields: %w", err)
	}

	return session, nil
}

// SetPart records the ETag of an accepted part and pushes the expiry of the
// upload forward, so an upload the account keeps feeding is not swept. A part
// for a session that is gone is refused instead of creating one, and so is a
// part whose new expiry has already passed: the sweep must be able to load the
// session after its expiry score falls due, which it cannot do once the hash is
// gone.
//
// The hashes outlive the expiry by _sessionGrace for that reason. The expiry
// score falls due first and the sweep then reads a session that is still there.
func (r *Repo) SetPart(
	ctx context.Context,
	uploadID string,
	partNumber int,
	etag string,
	expiresAt time.Time,
) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return entity.ErrUploadNotFound
	}

	ttl += _sessionGrace

	for {
		err := r.rdb.Client.Watch(ctx, func(tx *goredis.Tx) error {
			exists, existsErr := tx.Exists(ctx, sessionKey(uploadID)).Result()
			if existsErr != nil {
				return existsErr
			}

			if exists == 0 {
				return entity.ErrUploadNotFound
			}

			_, pipeErr := tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
				pipe.HSet(ctx, partsKey(uploadID), strconv.Itoa(partNumber), etag)
				pipe.HSet(ctx, sessionKey(uploadID), "expires_at", strconv.FormatInt(expiresAt.Unix(), 10))
				pipe.ZAdd(ctx, _expiryKey, goredis.Z{Score: float64(expiresAt.Unix()), Member: uploadID})
				pipe.Expire(ctx, partsKey(uploadID), ttl)
				pipe.Expire(ctx, sessionKey(uploadID), ttl)

				return nil
			})

			return pipeErr
		}, sessionKey(uploadID))
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}

		if err != nil {
			if errors.Is(err, entity.ErrUploadNotFound) {
				return err
			}

			return fmt.Errorf("upload - SetPart - watch: %w", err)
		}

		return nil
	}
}

// Parts returns the ETags of the parts received so far, keyed by part number.
// A session that received no part yet has an empty map.
func (r *Repo) Parts(ctx context.Context, uploadID string) (map[int]string, error) {
	fields, err := r.rdb.Client.HGetAll(ctx, partsKey(uploadID)).Result()
	if err != nil {
		return nil, fmt.Errorf("upload - Parts - r.rdb.Client.HGetAll: %w", err)
	}

	parts := make(map[int]string, len(fields))

	for field, etag := range fields {
		partNumber, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("upload - Parts - strconv.Atoi: %w", err)
		}

		parts[partNumber] = etag
	}

	return parts, nil
}

// ListByUser returns the sessions in flight for an account. An identifier left
// in the index by a session that expired first is dropped here, so the index
// cleans itself up instead of failing the call.
func (r *Repo) ListByUser(ctx context.Context, userID string) ([]entity.UploadSession, error) {
	index := userKey(userID)

	ids, err := r.rdb.Client.SMembers(ctx, index).Result()
	if err != nil {
		return nil, fmt.Errorf("upload - ListByUser - r.rdb.Client.SMembers: %w", err)
	}

	sessions := make([]entity.UploadSession, 0, len(ids))

	for _, id := range ids {
		session, err := r.Get(ctx, id)
		if err != nil {
			if !errors.Is(err, entity.ErrUploadNotFound) {
				return nil, fmt.Errorf("upload - ListByUser - r.Get: %w", err)
			}

			if err := r.rdb.Client.SRem(ctx, index, id).Err(); err != nil {
				return nil, fmt.Errorf("upload - ListByUser - r.rdb.Client.SRem: %w", err)
			}

			continue
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Expired returns the sessions due for a sweep by now, oldest first, at most
// limit of them. The session hash outlives its expiry by _sessionGrace, so a
// due session is still readable by the sweep. A score whose session is already
// gone is dropped; its entry in the account index cannot be removed here,
// because the identifier alone does not say which account indexed it, so
// ListByUser drops that stale member on the next attempt.
func (r *Repo) Expired(ctx context.Context, now time.Time, limit int) ([]entity.UploadSession, error) {
	ids, err := r.rdb.Client.ZRangeArgs(ctx, goredis.ZRangeArgs{
		Key:     _expiryKey,
		Start:   "-inf",
		Stop:    strconv.FormatInt(now.Unix(), 10),
		ByScore: true,
		Offset:  0,
		Count:   int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("upload - Expired - r.rdb.Client.ZRangeArgs: %w", err)
	}

	sessions := make([]entity.UploadSession, 0, len(ids))

	for _, id := range ids {
		session, err := r.Get(ctx, id)
		if err != nil {
			if !errors.Is(err, entity.ErrUploadNotFound) {
				return nil, fmt.Errorf("upload - Expired - r.Get: %w", err)
			}

			if err := r.rdb.Client.ZRem(ctx, _expiryKey, id).Err(); err != nil {
				return nil, fmt.Errorf("upload - Expired - r.rdb.Client.ZRem: %w", err)
			}

			continue
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Delete drops a session, its parts, its index entry and its expiry score. A
// session that is already gone is not an error, because the sweep and the
// abort path both call it.
func (r *Repo) Delete(ctx context.Context, uploadID string) error {
	userID, err := r.rdb.Client.HGet(ctx, sessionKey(uploadID), "user_id").Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("upload - Delete - r.rdb.Client.HGet: %w", err)
	}

	pipe := r.rdb.Client.TxPipeline()
	pipe.Del(ctx, sessionKey(uploadID), partsKey(uploadID))
	pipe.ZRem(ctx, _expiryKey, uploadID)

	if userID != "" {
		pipe.SRem(ctx, userKey(userID), uploadID)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("upload - Delete - pipe.Exec: %w", err)
	}

	return nil
}

// LockQuota takes the per-account lock a start holds while it measures the
// quota, so two starts cannot both see the same free space. The returned
// release gives the lock back; it is safe to call more than once.
func (r *Repo) LockQuota(ctx context.Context, userID string) (func(), error) {
	key := quotaLockKey(userID)
	token := uuid.NewString()

	ticker := time.NewTicker(_lockRetry)
	defer ticker.Stop()

	for {
		taken, err := r.rdb.Client.SetNX(ctx, key, token, _lockTTL).Result()
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("upload - LockQuota - wait: %w", ctx.Err())
			}

			return nil, fmt.Errorf("upload - LockQuota - r.rdb.Client.SetNX: %w", err)
		}

		if taken {
			stop := make(chan struct{})
			go r.renewLoop(key, token, stop)

			return r.releaseFunc(key, token, stop), nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("upload - LockQuota - wait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// renewLoop extends the lock until the release closes stop, so a slow
// completion or abort does not let the lock expire under the operation.
func (r *Repo) renewLoop(key, token string, stop <-chan struct{}) {
	ticker := time.NewTicker(_lockRenew)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.renewLock(key, token)
		}
	}
}

// renewLock extends the lease only while this caller still holds the lock.
func (r *Repo) renewLock(key, token string) {
	err := r.rdb.Client.Eval(
		context.Background(),
		_renewScript,
		[]string{key},
		token,
		_lockTTL.Milliseconds(),
	).Err()
	if err != nil {
		log.Printf("upload - LockQuota - renew: %v", err)
	}
}

// releaseFunc builds the function that gives a quota lock back. The caller
// releases without a request context, because a release that must finish
// cannot depend on the request that took the lock.
func (r *Repo) releaseFunc(key, token string, stop chan struct{}) func() {
	var once sync.Once

	return func() {
		once.Do(func() {
			close(stop)

			err := r.rdb.Client.Eval(context.Background(), _releaseScript, []string{key}, token).Err()
			if err != nil {
				log.Printf("upload - LockQuota - release: %v", err)
			}
		})
	}
}

// sessionFromFields builds a session from the hash fields Create wrote. The
// expiry score of the session lives in the expiry set, which the sweep reads;
// the hash carries the same instant as expires_at so a loaded session can say
// whether it is already due. A session written before that field existed comes
// back with a zero ExpiresAt, which callers treat as "no expiry recorded".
func sessionFromFields(uploadID string, fields map[string]string) (entity.UploadSession, error) {
	size, err := strconv.ParseInt(fields["size"], 10, 64)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("strconv.ParseInt(size): %w", err)
	}

	partSize, err := strconv.ParseInt(fields["part_size"], 10, 64)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("strconv.ParseInt(part_size): %w", err)
	}

	partCount, err := strconv.ParseInt(fields["part_count"], 10, 64)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("strconv.ParseInt(part_count): %w", err)
	}

	createdAt, err := strconv.ParseInt(fields["created_at"], 10, 64)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("strconv.ParseInt(created_at): %w", err)
	}

	expiresAt, err := parseExpiresAt(fields["expires_at"])
	if err != nil {
		return entity.UploadSession{}, err
	}

	return entity.UploadSession{
		ID:          uploadID,
		UserID:      fields["user_id"],
		FolderID:    fields["folder_id"],
		FileID:      fields["file_id"],
		Name:        fields["name"],
		ContentType: fields["content_type"],
		StorageKey:  fields["storage_key"],
		MultipartID: fields["multipart_id"],
		Size:        size,
		PartSize:    partSize,
		PartCount:   int(partCount),
		CreatedAt:   time.Unix(createdAt, 0).UTC(),
		ExpiresAt:   expiresAt,
	}, nil
}

// parseExpiresAt reads the stored expiry of a session. An absent field is a
// zero instant rather than an error, so a session written by an older build is
// still readable.
func parseExpiresAt(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("strconv.ParseInt(expires_at): %w", err)
	}

	return time.Unix(seconds, 0).UTC(), nil
}

// sessionKey builds the Redis key for a session.
func sessionKey(uploadID string) string {
	return _uploadPrefix + uploadID
}

// partsKey builds the Redis key for the parts a session received.
func partsKey(uploadID string) string {
	return _partsPrefix + uploadID
}

// userKey builds the Redis key for the index of the sessions of an account.
func userKey(userID string) string {
	return _userPrefix + userID
}

// quotaLockKey builds the Redis key for the quota lock of an account.
func quotaLockKey(userID string) string {
	return _quotaLockPrefix + userID
}
