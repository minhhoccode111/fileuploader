// Package file implements the drive file use case.
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

const (
	// _maxNameLength mirrors the width of the name columns of the folders and
	// files tables, so a generated name is rejected before it reaches them.
	_maxNameLength = 255

	// _defaultContentType is stored when the browser brings no usable type.
	_defaultContentType = "application/octet-stream"

	// _maxContentTypeLength is the width of the content type column.
	_maxContentTypeLength = 255

	// _presignTTL is how long a download link stays valid. It is long enough
	// for a slow start and short enough that a leaked link expires.
	_presignTTL = 15 * time.Minute

	// _maxDuplicateAttempts is how many numbered names a duplicate tries
	// before falling back to a timestamp.
	_maxDuplicateAttempts = 99
)

// UseCase -.
type UseCase struct {
	files    repo.FileRepo
	folders  repo.FolderRepo
	storage  repo.ObjectStorage
	uow      repo.UnitOfWork
	bucket   string
	maxBytes int64
}

// New returns a File use case.
func New(
	files repo.FileRepo,
	folders repo.FolderRepo,
	storage repo.ObjectStorage,
	uow repo.UnitOfWork,
	bucket string,
	maxBytes int64,
) usecase.File {
	return &UseCase{
		files:    files,
		folders:  folders,
		storage:  storage,
		uow:      uow,
		bucket:   bucket,
		maxBytes: maxBytes,
	}
}

// Get returns one file of the account.
func (uc *UseCase) Get(ctx context.Context, userID, fileID string) (entity.File, error) {
	file, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Get - uc.files.Get: %w", err)
	}

	return file, nil
}

// Upload stores one file: the bytes go into the bucket first and the row
// second, so a listed file always has an object behind it. The row is written
// under a lock on the folder, so two uploads cannot both claim one name; when
// the row cannot be written, the object is removed again.
func (uc *UseCase) Upload(
	ctx context.Context,
	userID, folderID, rawName, contentType string,
	size int64,
	body io.Reader,
) (entity.File, error) {
	name, err := entity.CleanName(rawName)
	if err != nil {
		return entity.File{}, err
	}

	if size > uc.maxBytes {
		return entity.File{}, entity.ErrFileTooLarge
	}

	contentType = normalizeContentType(contentType)

	_, err = uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Upload - uc.folders.Get: %w", err)
	}

	err = uc.checkName(ctx, userID, folderID, name)
	if err != nil {
		return entity.File{}, err
	}

	id := uuid.New().String()
	key := storageKey(userID, id)

	err = uc.storage.PutObject(ctx, uc.bucket, key, body, size, contentType)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Upload - uc.storage.PutObject: %w", err)
	}

	now := time.Now().UTC()

	file := entity.File{
		ID:          id,
		UserID:      userID,
		FolderID:    folderID,
		Name:        name,
		ContentType: contentType,
		StorageKey:  key,
		Size:        size,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = uc.storeRow(ctx, userID, folderID, name, &file)
	if err != nil {
		return entity.File{}, uc.finalizeError(ctx, &file, err)
	}

	return file, nil
}

// storeRow writes the row of an uploaded file under a lock on the folder, so
// the name check and the insert see the same set of entries.
func (uc *UseCase) storeRow(
	ctx context.Context,
	userID, folderID, name string,
	file *entity.File,
) error {
	return uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		_, lockErr := uc.folders.Lock(ctx, userID, folderID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.Lock: %w", lockErr)
		}

		checkErr := uc.checkName(ctx, userID, folderID, name)
		if checkErr != nil {
			return checkErr
		}

		storeErr := uc.files.Store(ctx, file)
		if storeErr != nil {
			return fmt.Errorf("uc.files.Store: %w", storeErr)
		}

		return nil
	})
}

// Rename changes the name a file is listed under. The object keeps its
// identifier key, so nothing in the bucket moves. The folder is locked while
// the name is checked and written, so a rename and an upload cannot both claim
// one name.
func (uc *UseCase) Rename(ctx context.Context, userID, fileID, rawName string) (entity.File, error) {
	name, err := entity.CleanName(rawName)
	if err != nil {
		return entity.File{}, err
	}

	file, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Rename - uc.files.Get: %w", err)
	}

	oldName := file.Name
	file.Name = name
	file.UpdatedAt = time.Now().UTC()

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		_, lockErr := uc.folders.Lock(ctx, userID, file.FolderID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.Lock: %w", lockErr)
		}

		if !strings.EqualFold(oldName, name) {
			checkErr := uc.checkName(ctx, userID, file.FolderID, name)
			if checkErr != nil {
				return checkErr
			}
		}

		renameErr := uc.files.Rename(ctx, &file)
		if renameErr != nil {
			return fmt.Errorf("uc.files.Rename: %w", renameErr)
		}

		return nil
	})
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Rename - uc.uow.Do: %w", err)
	}

	return file, nil
}

// Move changes the folder a file is listed in. The object key is built from
// identifiers, so nothing in the bucket moves. The destination folder is
// locked while the name is checked and the row is written, so a move and an
// upload cannot both claim one name in the destination. Moving into the
// current folder is a no-op.
func (uc *UseCase) Move(
	ctx context.Context,
	userID, fileID, folderID string,
) (entity.File, error) {
	file, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Move - uc.files.Get: %w", err)
	}

	_, err = uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Move - uc.folders.Get: %w", err)
	}

	if file.FolderID == folderID {
		return file, nil
	}

	now := time.Now().UTC()
	file.FolderID = folderID
	file.UpdatedAt = now

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		_, lockErr := uc.folders.Lock(ctx, userID, folderID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.Lock: %w", lockErr)
		}

		checkErr := uc.checkName(ctx, userID, folderID, file.Name)
		if checkErr != nil {
			return checkErr
		}

		moveErr := uc.files.Move(ctx, userID, fileID, folderID)
		if moveErr != nil {
			return fmt.Errorf("uc.files.Move: %w", moveErr)
		}

		return nil
	})
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Move - uc.uow.Do: %w", err)
	}

	return file, nil
}

// Duplicate copies one file into another folder, or into its own folder with
// ` (1)` appended to the name when a clash would occur. The bytes are
// server-side copied to a new object key, so both files exist independently.
// The destination folder is locked while the name is checked and the row is
// stored, so a duplicate and an upload cannot both claim one name.
func (uc *UseCase) Duplicate(
	ctx context.Context,
	userID, fileID, folderID string,
) (entity.File, error) {
	source, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Duplicate - uc.files.Get: %w", err)
	}

	_, err = uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Duplicate - uc.folders.Get: %w", err)
	}

	return uc.duplicateInTransaction(ctx, userID, folderID, &source, time.Now().UTC())
}

func (uc *UseCase) duplicateInTransaction(
	ctx context.Context,
	userID, folderID string,
	source *entity.File,
	now time.Time,
) (entity.File, error) {
	var (
		dup    entity.File
		newKey string
		copied bool
	)

	err := uc.uow.Do(ctx, func(ctx context.Context) error {
		if err := uc.folders.LockUser(ctx, userID); err != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", err)
		}

		if _, err := uc.folders.Lock(ctx, userID, folderID); err != nil {
			return fmt.Errorf("uc.folders.Lock: %w", err)
		}

		name, nameErr := uc.duplicateName(ctx, userID, folderID, source.Name, now)
		if nameErr != nil {
			return nameErr
		}

		newID := uuid.New().String()
		newKey = storageKey(userID, newID)

		if err := uc.storage.CopyObject(ctx, uc.bucket, newKey, source.StorageKey); err != nil {
			return fmt.Errorf("uc.storage.CopyObject: %w", err)
		}

		copied = true

		dup = entity.File{
			ID:          newID,
			UserID:      userID,
			FolderID:    folderID,
			Name:        name,
			ContentType: source.ContentType,
			StorageKey:  newKey,
			Size:        source.Size,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := uc.files.Store(ctx, &dup); err != nil {
			if removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, newKey); removeErr != nil {
				return fmt.Errorf("uc.files.Store: %w; cleanup: %w", err, removeErr)
			}

			copied = false

			return fmt.Errorf("uc.files.Store: %w", err)
		}

		return nil
	})
	if err != nil {
		return entity.File{}, uc.finalizeDuplicateError(ctx, err, newKey, copied)
	}

	return dup, nil
}

// finalizeDuplicateError removes the object a duplicate wrote when the transaction
// that should have recorded it fails. A cleanup failure is joined into the
// error, so the caller still sees the failure that mattered.
func (uc *UseCase) finalizeDuplicateError(ctx context.Context, err error, key string, copied bool) error {
	if !copied || key == "" {
		return fmt.Errorf("FileUseCase - Duplicate - uc.uow.Do: %w", err)
	}

	removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, key)
	if removeErr != nil {
		return fmt.Errorf(
			"FileUseCase - Duplicate - uc.uow.Do: %w; cleanup: %w",
			err,
			removeErr,
		)
	}

	return fmt.Errorf("FileUseCase - Duplicate - uc.uow.Do: %w", err)
}

// duplicateName returns a name that is free in the destination folder. The
// original name is tried first; when it is taken, ` (1)`, ` (2)` and so on are
// appended before the extension until a free name is found.
func (uc *UseCase) duplicateName(
	ctx context.Context,
	userID, folderID, name string,
	now time.Time,
) (string, error) {
	taken, err := uc.nameTakenInFolder(ctx, userID, folderID, name)
	if err != nil {
		return "", fmt.Errorf("FileUseCase - duplicateName - uc.nameTakenInFolder: %w", err)
	}

	if !taken {
		return name, nil
	}

	base, ext := splitName(name)

	for i := 1; i <= _maxDuplicateAttempts; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if utf8.RuneCountInString(candidate) > _maxNameLength {
			continue
		}

		taken, err := uc.nameTakenInFolder(ctx, userID, folderID, candidate)
		if err != nil {
			return "", fmt.Errorf("FileUseCase - duplicateName - uc.nameTakenInFolder: %w", err)
		}

		if !taken {
			return candidate, nil
		}
	}

	candidate := fmt.Sprintf("%s (%d)%s", base, now.Unix(), ext)
	if utf8.RuneCountInString(candidate) > _maxNameLength {
		return "", entity.ErrInvalidName
	}

	return candidate, nil
}

// nameTakenInFolder reports whether a folder or a file of the account already
// carries the name in the same parent.
func (uc *UseCase) nameTakenInFolder(ctx context.Context, userID, folderID, name string) (bool, error) {
	taken, err := uc.folders.NameTaken(ctx, userID, folderID, name)
	if err != nil || taken {
		return taken, err
	}

	return uc.files.NameTaken(ctx, userID, folderID, name)
}

// splitName splits a file name into the base and the extension including the
// dot. A name without an extension gets an empty extension.
func splitName(name string) (base, ext string) {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[:i], name[i:]
	}

	return name, ""
}

// Delete removes the row and then the object. The row goes first: a file that
// is no longer listed cannot be downloaded, while the opposite would leave a
// listed file without bytes behind it. A failed cleanup is marked, so the
// caller can log it and still report the delete as done.
func (uc *UseCase) Delete(ctx context.Context, userID, fileID string) (entity.File, error) {
	file, err := uc.files.Delete(ctx, userID, fileID)
	if err != nil {
		return entity.File{}, fmt.Errorf("FileUseCase - Delete - uc.files.Delete: %w", err)
	}

	err = uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, file.StorageKey)
	if err != nil {
		return file, fmt.Errorf(
			"FileUseCase - Delete - uc.storage.RemoveObject: %w",
			errors.Join(entity.ErrStorageCleanup, err),
		)
	}

	return file, nil
}

// Download returns a time-limited link to the object, handed to the browser as
// a download under the name the file carries now.
func (uc *UseCase) Download(ctx context.Context, userID, fileID string) (string, error) {
	file, err := uc.files.Get(ctx, userID, fileID)
	if err != nil {
		return "", fmt.Errorf("FileUseCase - Download - uc.files.Get: %w", err)
	}

	link, err := uc.storage.PresignedGetObject(
		ctx,
		uc.bucket,
		file.StorageKey,
		_presignTTL,
		file.Name,
	)
	if err != nil {
		return "", fmt.Errorf("FileUseCase - Download - uc.storage.PresignedGetObject: %w", err)
	}

	return link, nil
}

// checkName rejects a name another entry of the folder already carries. A
// listing shows folders and files in one list, so the check spans both.
func (uc *UseCase) checkName(ctx context.Context, userID, folderID, name string) error {
	taken, err := uc.folders.NameTaken(ctx, userID, folderID, name)
	if err != nil {
		return fmt.Errorf("FileUseCase - checkName - uc.folders.NameTaken: %w", err)
	}

	if taken {
		return entity.ErrEntryAlreadyExists
	}

	taken, err = uc.files.NameTaken(ctx, userID, folderID, name)
	if err != nil {
		return fmt.Errorf("FileUseCase - checkName - uc.files.NameTaken: %w", err)
	}

	if taken {
		return entity.ErrEntryAlreadyExists
	}

	return nil
}

// finalizeError removes the object an upload just wrote when the row that
// should point at it cannot be stored. A cleanup failure is joined into the
// error, so the caller still sees the failure that mattered.
func (uc *UseCase) finalizeError(ctx context.Context, file *entity.File, storeErr error) error {
	removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, file.StorageKey)
	if removeErr != nil {
		return fmt.Errorf(
			"FileUseCase - Upload - uc.uow.Do: %w",
			errors.Join(storeErr, removeErr, entity.ErrStorageCleanup),
		)
	}

	return fmt.Errorf("FileUseCase - Upload - uc.uow.Do: %w", storeErr)
}

// normalizeContentType keeps a media type the browser sent when it is one, and
// falls back to the generic type otherwise, so a broken header never reaches
// the object metadata.
func normalizeContentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > _maxContentTypeLength {
		return _defaultContentType
	}

	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return _defaultContentType
	}

	return mediaType
}

// storageKey builds the object key of a file. It holds identifiers only, so a
// rename never has to move or copy the stored object.
func storageKey(userID, fileID string) string {
	return "users/" + userID + "/files/" + fileID
}
