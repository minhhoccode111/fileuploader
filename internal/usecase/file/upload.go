package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase/pace"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

const (
	// _uploadPartTargetSeconds is how long one part should take at the
	// account's upload rate. It stays well inside the proxy and server
	// timeouts, so a slow account still finishes each part.
	_uploadPartTargetSeconds = 90

	// _uploadPartFloor is the smallest part S3 accepts, apart from the last
	// part of an upload, which may be shorter.
	_uploadPartFloor = int64(5 << 20)

	// _uploadPartCeiling is the largest part one upload asks for.
	_uploadPartCeiling = int64(50 << 20)

	// _sweepBatch is how many expired sessions one sweep query returns. The
	// sweep asks again until the store has none left, so the batch only caps
	// how much it holds at once.
	_sweepBatch = 100

	// _quotaLockWait bounds how long an operation waits for the per-account
	// quota lock. The holder renews its lease, so a stuck holder could
	// otherwise stall the caller, and the sweep behind it, without end.
	_quotaLockWait = 10 * time.Second

	// _completionTTL is how long the marker of a finished upload answers a
	// retried completion. A retry follows a lost response, so seconds matter
	// and hours are plenty.
	_completionTTL = time.Hour

	// _startLimit is how many uploads one account may open per _startWindow.
	// A whole folder of small files still fits, while a flood cannot reserve
	// the quota in one burst.
	_startLimit = 20

	// _startWindow is the window the start counter of an account covers.
	_startWindow = time.Minute

	// _staleUploadAge is how long a multipart upload the app lost track of may
	// sit in the bucket before the sweep aborts it. MinIO has no lifecycle
	// action for this, so the app applies the wait itself.
	_staleUploadAge = 24 * time.Hour
)

// StartUpload opens the multipart upload of one file, reserves its declared
// size against the account quota, and stores the session the later parts join.
// The full size is reserved at start, so two uploads cannot both claim the
// last headroom of an account. The reservation lives as long as the session:
// completion, abort and the sweeper release it.
//
//nolint:gocyclo,cyclop,funlen // the start walks validation, quota, storage and the session store in order
func (uc *UseCase) StartUpload(
	ctx context.Context,
	userID, folderID, rawName, contentType string,
	size int64,
) (entity.UploadSession, error) {
	name, err := entity.CleanName(rawName)
	if err != nil {
		return entity.UploadSession{}, err
	}

	if size < 0 {
		return entity.UploadSession{}, entity.ErrInvalidUpload
	}

	if size > uc.maxBytes {
		return entity.UploadSession{}, entity.ErrFileTooLarge
	}

	allowed, err := uc.uploads.AllowStart(ctx, userID, _startLimit, _startWindow)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf(
			"FileUseCase - StartUpload - uc.uploads.AllowStart: %w",
			err,
		)
	}

	if !allowed {
		return entity.UploadSession{}, entity.ErrUploadRateLimited
	}

	_, err = uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("FileUseCase - StartUpload - uc.folders.Get: %w", err)
	}

	err = uc.checkName(ctx, userID, folderID, name)
	if err != nil {
		return entity.UploadSession{}, err
	}

	quota, rate, err := uc.uploadAllowance(ctx, userID)
	if err != nil {
		return entity.UploadSession{}, err
	}

	partSize := partSizeForRate(rate, uc.partBytes)

	partCount, err := entity.UploadPartCount(size, partSize)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf(
			"FileUseCase - StartUpload - entity.UploadPartCount: %w",
			err,
		)
	}

	release, err := uc.reserveUploadQuota(ctx, userID, size, quota)
	if err != nil {
		return entity.UploadSession{}, err
	}

	defer release()

	fileID := uuid.NewString()
	uploadID := uuid.NewString()
	key := storageKey(userID, fileID)
	mediaType := normalizeContentType(contentType)

	multipartID, err := uc.openMultipart(ctx, size, key, mediaType)
	if err != nil {
		return entity.UploadSession{}, err
	}

	now := time.Now().UTC()

	session := entity.UploadSession{
		ID:          uploadID,
		UserID:      userID,
		FolderID:    folderID,
		FileID:      fileID,
		Name:        name,
		ContentType: mediaType,
		StorageKey:  key,
		MultipartID: multipartID,
		Size:        size,
		PartSize:    partSize,
		PartCount:   partCount,
		CreatedAt:   now,
		ExpiresAt:   now.Add(uc.sessionIdleTTL),
	}

	err = uc.uploads.Create(ctx, session, uc.sessionIdleTTL)
	if err != nil {
		return entity.UploadSession{}, uc.startError(ctx, key, multipartID, err)
	}

	return session, nil
}

// openMultipart opens the multipart upload one file needs. An empty file has no
// parts to join, so it is written in one request at completion and needs no
// multipart upload to abort or sweep.
func (uc *UseCase) openMultipart(
	ctx context.Context,
	size int64,
	key, contentType string,
) (string, error) {
	if size == 0 {
		return "", nil
	}

	multipartID, err := uc.storage.CreateMultipartUpload(ctx, uc.bucket, key, contentType)
	if err != nil {
		return "", fmt.Errorf(
			"FileUseCase - StartUpload - uc.storage.CreateMultipartUpload: %w",
			err,
		)
	}

	return multipartID, nil
}

// uploadAllowance resolves the quota and upload rate the account is held to.
// A use case without a user repository has no limits.
func (uc *UseCase) uploadAllowance(ctx context.Context, userID string) (quota, rate int64, err error) {
	if uc.users == nil {
		return 0, uc.defaultUploadRate, nil
	}

	user, err := uc.users.GetByID(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("FileUseCase - StartUpload - uc.users.GetByID: %w", err)
	}

	return user.EffectiveStorageQuota(uc.defaultQuota), user.EffectiveUploadRate(uc.defaultUploadRate), nil
}

// UploadPart stores one numbered part of an open upload. The part must carry
// the size the session expects, and the bytes are paced at the account's
// upload rate on their way to the bucket. The session expiry slides forward
// only after the bucket accepted the part, so a failed write leaves the
// session exactly where it was.
func (uc *UseCase) UploadPart(
	ctx context.Context,
	userID, uploadID string,
	partNumber int,
	body io.Reader,
	size int64,
) error {
	session, err := uc.ownedSession(ctx, userID, uploadID, "UploadPart")
	if err != nil {
		return err
	}

	expected, err := session.ExpectedPartSize(partNumber)
	if err != nil {
		return err
	}

	if size != expected {
		return entity.ErrUploadPartMismatch
	}

	rate, err := uc.uploadRate(ctx, userID)
	if err != nil {
		return err
	}

	reader := body
	if rate > 0 && uc.pacer != nil {
		reader = pace.NewReader(ctx, body, uc.pacer, userID, repo.TransferUpload, rate)
	}

	etag, err := uc.storage.UploadPart(
		ctx,
		uc.bucket,
		session.StorageKey,
		session.MultipartID,
		partNumber,
		reader,
		size,
	)
	if err != nil {
		return fmt.Errorf("FileUseCase - UploadPart - uc.storage.UploadPart: %w", err)
	}

	err = uc.slideSession(ctx, uploadID, partNumber, etag)
	if err != nil {
		return err
	}

	return nil
}

// AbortUpload ends one open upload on behalf of its owner: the multipart upload
// is aborted so no orphan parts stay in the bucket, and the session is deleted
// so its quota reservation is released. A session that is already gone is not an
// error, because the abort path and the sweeper both call it.
func (uc *UseCase) AbortUpload(ctx context.Context, userID, uploadID string) error {
	session, err := uc.uploads.Get(ctx, uploadID)
	if err != nil {
		if errors.Is(err, entity.ErrUploadNotFound) {
			return nil
		}

		return fmt.Errorf("FileUseCase - AbortUpload - uc.uploads.Get: %w", err)
	}

	if session.UserID != userID {
		return entity.ErrUploadNotFound
	}

	release, err := uc.lockQuota(ctx, userID, "AbortUpload")
	if err != nil {
		return err
	}

	defer release()

	return uc.discardSession(ctx, &session, "AbortUpload")
}

// SweepUploads releases the sessions an idle account left behind and aborts
// the multipart uploads the bucket still holds for sessions that are gone. The
// session pass takes one page of expired sessions, aborts the multipart upload
// of each and deletes the session, which releases the reservation it held. A
// pass that deletes nothing stops the session sweep: the same expired session
// would otherwise be fetched forever. A session that survives a failed abort or
// delete is left for the next sweep, and its error is joined into the result.
func (uc *UseCase) SweepUploads(ctx context.Context) (int, error) {
	swept, errs := uc.sweepExpiredSessions(ctx)

	stale, staleErrs := uc.sweepStaleUploads(ctx)

	swept += stale

	errs = append(errs, staleErrs...)

	return swept, errors.Join(errs...)
}

// sweepExpiredSessions removes the sessions due by now and reports how many it
// released, together with the failures of the ones it kept.
func (uc *UseCase) sweepExpiredSessions(ctx context.Context) (int, []error) {
	swept := 0

	var errs []error

	for {
		sessions, err := uc.uploads.Expired(ctx, time.Now().UTC(), _sweepBatch)
		if err != nil {
			errs = append(errs, fmt.Errorf("FileUseCase - SweepUploads - uc.uploads.Expired: %w", err))

			break
		}

		if len(sessions) == 0 {
			break
		}

		deleted, batchErrs := uc.sweepBatch(ctx, sessions)
		swept += deleted

		errs = append(errs, batchErrs...)

		if deleted == 0 {
			break
		}
	}

	return swept, errs
}

// sweepStaleUploads aborts the multipart uploads past the stale age that no
// session claims any more. A slow upload with a live session stays, because the
// session sweep already owns every upload the app knows about.
func (uc *UseCase) sweepStaleUploads(ctx context.Context) (int, []error) {
	uploads, err := uc.storage.ListIncompleteUploads(ctx, uc.bucket)
	if err != nil {
		return 0, []error{
			fmt.Errorf("FileUseCase - SweepUploads - uc.storage.ListIncompleteUploads: %w", err),
		}
	}

	deadline := time.Now().Add(-_staleUploadAge)

	aborted := 0

	var errs []error

	for i := range uploads {
		if uploads[i].Initiated.After(deadline) {
			continue
		}

		live, sessionErr := uc.uploadHasSession(ctx, uploads[i].UploadID)
		if sessionErr != nil {
			errs = append(errs, sessionErr)

			continue
		}

		if live {
			continue
		}

		err := uc.storage.AbortMultipartUpload(
			ctx,
			uc.bucket,
			uploads[i].Key,
			uploads[i].UploadID,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"FileUseCase - SweepUploads - uc.storage.AbortMultipartUpload: %w",
				err,
			))

			continue
		}

		aborted++
	}

	return aborted, errs
}

// uploadHasSession reports whether the store still knows an upload. A store
// failure is returned as an error, and the caller keeps the multipart: aborting
// an upload that may still be active is worse than waiting one pass.
func (uc *UseCase) uploadHasSession(ctx context.Context, uploadID string) (bool, error) {
	_, err := uc.uploads.Get(ctx, uploadID)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, entity.ErrUploadNotFound) {
		return false, nil
	}

	return false, fmt.Errorf("FileUseCase - SweepUploads - uc.uploads.Get: %w", err)
}

// sweepBatch aborts and deletes one page of expired sessions and reports how
// many it removed, together with the failures of the ones it kept.
func (uc *UseCase) sweepBatch(
	ctx context.Context,
	sessions []entity.UploadSession,
) (int, []error) {
	deleted := 0

	var errs []error

	for i := range sessions {
		err := uc.sweepSession(ctx, &sessions[i])
		if err != nil {
			errs = append(errs, err)

			continue
		}

		deleted++
	}

	return deleted, errs
}

// sweepSession releases one expired session while holding the quota lock of its
// owner, so a concurrent start cannot measure the quota mid-release.
func (uc *UseCase) sweepSession(ctx context.Context, session *entity.UploadSession) error {
	release, err := uc.lockQuota(ctx, session.UserID, "SweepUploads")
	if err != nil {
		return err
	}

	defer release()

	return uc.discardSession(ctx, session, "SweepUploads")
}

// discardSession aborts the multipart upload of a session and deletes the
// session itself. A session that never opened a multipart upload, an empty file
// for example, has nothing to abort. A failed abort keeps the session, so a
// later abort or sweep still finds the bucket object and can finish the job.
func (uc *UseCase) discardSession(
	ctx context.Context,
	session *entity.UploadSession,
	op string,
) error {
	if session.MultipartID != "" {
		err := uc.storage.AbortMultipartUpload(ctx, uc.bucket, session.StorageKey, session.MultipartID)
		if err != nil {
			return fmt.Errorf("FileUseCase - %s - uc.storage.AbortMultipartUpload: %w", op, err)
		}
	}

	err := uc.uploads.Delete(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("FileUseCase - %s - uc.uploads.Delete: %w", op, err)
	}

	return nil
}

// slideSession records the accepted part and pushes the session expiry one idle
// window forward, so an upload that keeps sending parts never expires.
func (uc *UseCase) slideSession(ctx context.Context, uploadID string, partNumber int, etag string) error {
	err := uc.uploads.SetPart(
		ctx,
		uploadID,
		partNumber,
		etag,
		time.Now().UTC().Add(uc.sessionIdleTTL),
	)
	if err != nil {
		return fmt.Errorf("FileUseCase - UploadPart - uc.uploads.SetPart: %w", err)
	}

	return nil
}

// CompleteUpload joins the parts of one open upload into a single object and
// records the file that points at it. The size reserved at start becomes stored
// usage, so the per-account quota lock is held from the join until the row is
// written. A completion that cannot record the row removes the joined object
// and ends the session, so no phantom reservation is left behind. A completion
// retried after its response was lost answers with the file the first call
// stored.
func (uc *UseCase) CompleteUpload(ctx context.Context, userID, uploadID string) (entity.File, error) {
	file, done, err := uc.completedUpload(ctx, userID, uploadID)
	if err != nil {
		return entity.File{}, err
	}

	if done {
		return file, nil
	}

	session, err := uc.ownedSession(ctx, userID, uploadID, "CompleteUpload")
	if err != nil {
		return entity.File{}, err
	}

	if _, err := uc.sessionParts(ctx, &session); err != nil {
		return entity.File{}, err
	}

	release, err := uc.lockQuota(ctx, userID, "CompleteUpload")
	if err != nil {
		return entity.File{}, err
	}

	defer release()

	// Re-read under the lock: a concurrent completion or abort can delete the
	// session while this request waits, and using the stale copy would let the
	// loser remove the object the winner already stored. A session that is gone
	// now may have been completed while this request waited, so the marker is
	// checked again before the request gives up.
	session, err = uc.ownedSession(ctx, userID, uploadID, "CompleteUpload")
	if err != nil {
		return uc.retryCompletion(ctx, userID, uploadID, err)
	}

	parts, err := uc.sessionParts(ctx, &session)
	if err != nil {
		return entity.File{}, err
	}

	err = uc.joinParts(ctx, &session, parts)
	if err != nil {
		err = uc.joinError(ctx, &session, err)
		uc.endSession(ctx, session.ID)

		return entity.File{}, err
	}

	file = fileFromSession(&session)

	err = uc.storeRow(ctx, session.UserID, session.FolderID, session.Name, &file)
	if err != nil {
		finalErr := uc.finalizeError(ctx, &file, err)
		uc.endSession(ctx, session.ID)

		return entity.File{}, fmt.Errorf("FileUseCase - CompleteUpload - uc.storeRow: %w", finalErr)
	}

	uc.markCompleted(ctx, session.ID, file.ID)
	uc.endSession(ctx, session.ID)

	return file, nil
}

// completedUpload returns the file an earlier completion of this upload
// stored. A missing marker, or one whose file was deleted afterwards, reports
// false so the caller falls back to the session.
func (uc *UseCase) completedUpload(
	ctx context.Context,
	userID, uploadID string,
) (entity.File, bool, error) {
	fileID, err := uc.uploads.Completion(ctx, uploadID)
	if err != nil {
		if errors.Is(err, entity.ErrUploadNotFound) {
			return entity.File{}, false, nil
		}

		return entity.File{}, false, fmt.Errorf(
			"FileUseCase - CompleteUpload - uc.uploads.Completion: %w",
			err,
		)
	}

	file, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		if errors.Is(err, entity.ErrFileNotFound) {
			return entity.File{}, false, nil
		}

		return entity.File{}, false, fmt.Errorf("FileUseCase - CompleteUpload - uc.files.Get: %w", err)
	}

	return file, true, nil
}

// retryCompletion answers a completion whose session disappeared while it
// waited for the quota lock. A marker written by the completion that won the
// race turns the retry into a success; without one the original failure stands.
func (uc *UseCase) retryCompletion(
	ctx context.Context,
	userID, uploadID string,
	sessionErr error,
) (entity.File, error) {
	file, done, err := uc.completedUpload(ctx, userID, uploadID)
	if err != nil {
		return entity.File{}, err
	}

	if done {
		return file, nil
	}

	return entity.File{}, sessionErr
}

// markCompleted records which file the upload produced. A failed marker only
// costs a retried completion its answer, so it never fails the completion that
// already stored the row.
func (uc *UseCase) markCompleted(ctx context.Context, uploadID, fileID string) {
	err := uc.uploads.MarkCompleted(context.WithoutCancel(ctx), uploadID, fileID, _completionTTL)
	if err != nil {
		// The upload itself stays successful: only a retried completion loses
		// its answer, and the log explains the later 404.
		log.Printf("FileUseCase - markCompleted - uc.uploads.MarkCompleted: %v", err)
	}
}

// sessionParts returns the ETags of a session in part order and refuses a
// session that is not fully uploaded yet. The store joins the parts by number,
// so a gap means the browser still owes bytes.
func (uc *UseCase) sessionParts(
	ctx context.Context,
	session *entity.UploadSession,
) ([]multipart.Part, error) {
	stored, err := uc.uploads.Parts(ctx, session.ID)
	if err != nil {
		return nil, fmt.Errorf("FileUseCase - CompleteUpload - uc.uploads.Parts: %w", err)
	}

	if len(stored) != session.PartCount {
		return nil, entity.ErrUploadIncomplete
	}

	parts := make([]multipart.Part, 0, session.PartCount)

	for number := 1; number <= session.PartCount; number++ {
		etag, ok := stored[number]
		if !ok || etag == "" {
			return nil, entity.ErrUploadIncomplete
		}

		parts = append(parts, multipart.Part{Number: number, ETag: etag})
	}

	return parts, nil
}

// joinParts turns the uploaded parts into the final object. An empty upload has
// no parts to join, so the empty object is written directly.
func (uc *UseCase) joinParts(
	ctx context.Context,
	session *entity.UploadSession,
	parts []multipart.Part,
) error {
	if session.Size == 0 {
		err := uc.storage.PutObject(
			ctx,
			uc.bucket,
			session.StorageKey,
			bytes.NewReader(nil),
			0,
			session.ContentType,
		)
		if err != nil {
			return fmt.Errorf("FileUseCase - CompleteUpload - uc.storage.PutObject: %w", err)
		}

		return nil
	}

	err := uc.storage.CompleteMultipartUpload(
		ctx,
		uc.bucket,
		session.StorageKey,
		session.MultipartID,
		parts,
	)
	if err != nil {
		return fmt.Errorf(
			"FileUseCase - CompleteUpload - uc.storage.CompleteMultipartUpload: %w",
			err,
		)
	}

	return nil
}

// joinError aborts the multipart upload a failed join leaves behind, so the
// parts already in the bucket do not outlive the session that could have
// completed them. A failed abort is joined with the cleanup sentinel, so the
// caller can tell the bucket may still hold the parts.
func (uc *UseCase) joinError(
	ctx context.Context,
	session *entity.UploadSession,
	joinErr error,
) error {
	if session.MultipartID == "" {
		return joinErr
	}

	abortErr := uc.storage.AbortMultipartUpload(
		context.WithoutCancel(ctx),
		uc.bucket,
		session.StorageKey,
		session.MultipartID,
	)
	if abortErr != nil {
		return fmt.Errorf(
			"FileUseCase - CompleteUpload - uc.storage.AbortMultipartUpload: %w",
			errors.Join(joinErr, abortErr, entity.ErrStorageCleanup),
		)
	}

	return joinErr
}

// endSession removes the session a finished upload leaves behind, which is what
// releases its quota reservation. A failed delete is ignored: the sweeper finds
// the expired session and releases it later.
func (uc *UseCase) endSession(ctx context.Context, uploadID string) {
	//nolint:errcheck // the sweeper releases a session that outlives its upload
	uc.uploads.Delete(context.WithoutCancel(ctx), uploadID)
}

// lockQuota takes the per-account quota lock under a bounded wait. A wait that
// runs out reports the busy sentinel, so a request can answer 503 and the sweep
// can move on to the next account.
func (uc *UseCase) lockQuota(ctx context.Context, userID, op string) (func(), error) {
	waitCtx, cancel := context.WithTimeout(ctx, _quotaLockWait)

	release, err := uc.uploads.LockQuota(waitCtx, userID)
	if err != nil {
		cancel()

		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf(
				"FileUseCase - %s - uc.uploads.LockQuota: %w",
				op,
				entity.ErrUploadQuotaBusy,
			)
		}

		return nil, fmt.Errorf("FileUseCase - %s - uc.uploads.LockQuota: %w", op, err)
	}

	return func() {
		cancel()
		release()
	}, nil
}

// ownedSession loads a session for one account and refuses a missing one, one
// of another account, and one whose idle window has passed. The three refusals
// share one sentinel, so a caller cannot tell an expired upload from one it
// never owned.
func (uc *UseCase) ownedSession(
	ctx context.Context,
	userID, uploadID, op string,
) (entity.UploadSession, error) {
	session, err := uc.uploads.Get(ctx, uploadID)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf("FileUseCase - %s - uc.uploads.Get: %w", op, err)
	}

	if session.UserID != userID || sessionExpired(&session) {
		return entity.UploadSession{}, entity.ErrUploadNotFound
	}

	return session, nil
}

// sessionExpired reports whether the idle window of a session has passed. A
// session that carries no expiry is never treated as expired, so a session
// written before the store recorded one can still finish.
func sessionExpired(session *entity.UploadSession) bool {
	return !session.ExpiresAt.IsZero() && !session.ExpiresAt.After(time.Now())
}

// fileFromSession builds the row of a completed upload. The identifiers and the
// key come from the session, so the object the parts were joined under is the
// object the row points at.
func fileFromSession(session *entity.UploadSession) entity.File {
	return entity.File{
		ID:          session.FileID,
		UserID:      session.UserID,
		FolderID:    session.FolderID,
		Name:        session.Name,
		ContentType: session.ContentType,
		StorageKey:  session.StorageKey,
		Size:        session.Size,
		CreatedAt:   session.CreatedAt,
		UpdatedAt:   time.Now().UTC(),
	}
}

// uploadRate resolves the rate the parts of an account are paced at. A use
// case without a user repository falls back to the default rate, and a rate of
// zero means the account is not paced at all.
func (uc *UseCase) uploadRate(ctx context.Context, userID string) (int64, error) {
	if uc.users == nil {
		return uc.defaultUploadRate, nil
	}

	user, err := uc.users.GetByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("FileUseCase - UploadPart - uc.users.GetByID: %w", err)
	}

	return user.EffectiveUploadRate(uc.defaultUploadRate), nil
}

// reserveUploadQuota takes the per-account quota lock and admits the declared
// size only when the stored usage plus the sizes of the live uploads leave
// room. It returns the release the caller defers; every error path has already
// released the lock.
func (uc *UseCase) reserveUploadQuota(
	ctx context.Context,
	userID string,
	size, quota int64,
) (func(), error) {
	if quota <= 0 {
		return func() {}, nil
	}

	release, err := uc.lockQuota(ctx, userID, "StartUpload")
	if err != nil {
		return nil, err
	}

	used, err := uc.storageUsed(ctx, userID)
	if err != nil {
		release()

		return nil, err
	}

	reserved, err := uc.uploadReserved(ctx, userID)
	if err != nil {
		release()

		return nil, err
	}

	if used+reserved+size > quota {
		release()

		return nil, entity.ErrStorageQuotaExceeded
	}

	return release, nil
}

// storageUsed reports the bytes the account already stores.
func (uc *UseCase) storageUsed(ctx context.Context, userID string) (int64, error) {
	if uc.users == nil {
		return 0, nil
	}

	user, err := uc.users.GetByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("FileUseCase - StartUpload - uc.users.GetByID: %w", err)
	}

	return user.StorageUsedBytes, nil
}

// uploadReserved sums the sizes the live sessions of the account hold.
func (uc *UseCase) uploadReserved(ctx context.Context, userID string) (int64, error) {
	sessions, err := uc.uploads.ListByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("FileUseCase - StartUpload - uc.uploads.ListByUser: %w", err)
	}

	var reserved int64

	for i := range sessions {
		reserved += sessions[i].Size
	}

	return reserved, nil
}

// startError aborts the multipart upload a failed start opened, so no orphan
// parts stay in the bucket for a session that does not exist. A start that
// opened no multipart upload, an empty file for example, has nothing to abort.
// A failed abort is joined into the error, so the caller still sees the failure
// that mattered.
func (uc *UseCase) startError(
	ctx context.Context,
	key, multipartID string,
	createErr error,
) error {
	if multipartID != "" {
		abortErr := uc.storage.AbortMultipartUpload(
			context.WithoutCancel(ctx),
			uc.bucket,
			key,
			multipartID,
		)
		if abortErr != nil {
			return fmt.Errorf(
				"FileUseCase - StartUpload - uc.uploads.Create: %w",
				errors.Join(createErr, abortErr, entity.ErrStorageCleanup),
			)
		}
	}

	return fmt.Errorf("FileUseCase - StartUpload - uc.uploads.Create: %w", createErr)
}

// partSizeForRate returns the part size one upload asks the browser to send. A
// positive rate shrinks the part so the sender needs about 90 seconds for it,
// which keeps one request inside the proxy and server timeouts. The result
// never drops below the part S3 accepts nor rises above the preferred size.
func partSizeForRate(rate, preferred int64) int64 {
	if preferred <= 0 || preferred > _uploadPartCeiling {
		preferred = _uploadPartCeiling
	}

	if preferred < _uploadPartFloor {
		preferred = _uploadPartFloor
	}

	if rate <= 0 {
		return preferred
	}

	target := rate * _uploadPartTargetSeconds
	if target < _uploadPartFloor {
		return _uploadPartFloor
	}

	if target < preferred {
		return target
	}

	return preferred
}
