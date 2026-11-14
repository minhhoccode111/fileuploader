package file

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
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
