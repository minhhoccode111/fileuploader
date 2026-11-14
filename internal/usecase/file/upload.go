package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

const (
	// _uploadPartTargetSeconds is how long one part should take at the
	// account's upload rate. It stays well inside the proxy and server
	// timeouts, so a slow account still finishes each part.
	_uploadPartTargetSeconds = 90

	// _paceBatchDivisor splits one second of an account's rate into the
	// batches a paced read hands over, so the limiter is consulted a few
	// times a second instead of once per kilobyte.
	_paceBatchDivisor = 4

	// _paceBatchFloor is the smallest batch a paced read hands over. Below
	// it the reservations are too fine-grained to matter.
	_paceBatchFloor = int64(64 << 10)

	// _paceBatchCeiling is the largest batch a paced read hands over, so a
	// slow sender is never granted more than a megabyte at once.
	_paceBatchCeiling = int64(1 << 20)

	// _uploadPartFloor is the smallest part S3 accepts, apart from the last
	// part of an upload, which may be shorter.
	_uploadPartFloor = int64(5 << 20)

	// _uploadPartCeiling is the largest part one upload asks for.
	_uploadPartCeiling = int64(50 << 20)
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

	multipartID, err := uc.storage.CreateMultipartUpload(ctx, uc.bucket, key, mediaType)
	if err != nil {
		return entity.UploadSession{}, fmt.Errorf(
			"FileUseCase - StartUpload - uc.storage.CreateMultipartUpload: %w",
			err,
		)
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
	session, err := uc.uploads.Get(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("FileUseCase - UploadPart - uc.uploads.Get: %w", err)
	}

	if session.UserID != userID {
		return entity.ErrUploadNotFound
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
		reader = &pacedReader{
			r:       body,
			limiter: uc.pacer,
			ctx:     ctx,
			userID:  userID,
			rate:    rate,
			batch:   paceBatch(rate),
		}
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
// and ends the session, so no phantom reservation is left behind.
func (uc *UseCase) CompleteUpload(ctx context.Context, userID, uploadID string) (entity.File, error) {
	session, err := uc.uploads.Get(ctx, uploadID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - CompleteUpload - uc.uploads.Get: %w", err)
	}

	if session.UserID != userID {
		return entity.File{}, entity.ErrUploadNotFound
	}

	parts, err := uc.sessionParts(ctx, &session)
	if err != nil {
		return entity.File{}, err
	}

	release, err := uc.uploads.LockQuota(ctx, userID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - CompleteUpload - uc.uploads.LockQuota: %w", err)
	}

	defer release()

	err = uc.joinParts(ctx, &session, parts)
	if err != nil {
		uc.endSession(ctx, session.ID)

		return entity.File{}, err
	}

	file := fileFromSession(&session)

	err = uc.storeRow(ctx, session.UserID, session.FolderID, session.Name, &file)
	if err != nil {
		finalErr := uc.finalizeError(ctx, &file, err)
		uc.endSession(ctx, session.ID)

		return entity.File{}, fmt.Errorf("FileUseCase - CompleteUpload - uc.storeRow: %w", finalErr)
	}

	uc.endSession(ctx, session.ID)

	return file, nil
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

// endSession removes the session a finished upload leaves behind, which is what
// releases its quota reservation. A failed delete is ignored: the sweeper finds
// the expired session and releases it later.
func (uc *UseCase) endSession(ctx context.Context, uploadID string) {
	//nolint:errcheck // the sweeper releases a session that outlives its upload
	uc.uploads.Delete(context.WithoutCancel(ctx), uploadID)
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

// pacedReader wraps the body of one part and reserves every batch it hands
// over at the account's rate. It reads in batches, so a fast sender cannot
// outrun the limiter between two reservations.
type pacedReader struct {
	r       io.Reader
	limiter repo.TransferLimiter
	ctx     context.Context
	userID  string
	rate    int64
	batch   int
}

// Read hands over at most one batch and waits for the limiter before the bytes
// reach the caller. A failed reservation is reported with the bytes already
// read, so the caller still sees the read that made progress.
func (p *pacedReader) Read(buf []byte) (int, error) {
	if len(buf) > p.batch {
		buf = buf[:p.batch]
	}

	n, err := p.r.Read(buf)
	if n == 0 {
		return n, err
	}

	paceErr := p.limiter.Pace(p.ctx, p.userID, int64(n), p.rate)
	if paceErr != nil {
		return n, paceErr
	}

	return n, err
}

// paceBatch returns how many bytes one paced read hands over. The batch is a
// quarter of a second of the account's rate, held between the floor and the
// ceiling so every rate gets a workable batch.
func paceBatch(rate int64) int {
	batch := min(max(rate/_paceBatchDivisor, _paceBatchFloor), _paceBatchCeiling)

	return int(batch)
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

	release, err := uc.uploads.LockQuota(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("FileUseCase - StartUpload - uc.uploads.LockQuota: %w", err)
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
// parts stay in the bucket for a session that does not exist. A failed abort is
// joined into the error, so the caller still sees the failure that mattered.
func (uc *UseCase) startError(
	ctx context.Context,
	key, multipartID string,
	createErr error,
) error {
	abortErr := uc.storage.AbortMultipartUpload(context.WithoutCancel(ctx), uc.bucket, key, multipartID)
	if abortErr != nil {
		return fmt.Errorf(
			"FileUseCase - StartUpload - uc.uploads.Create: %w",
			errors.Join(createErr, abortErr, entity.ErrStorageCleanup),
		)
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
