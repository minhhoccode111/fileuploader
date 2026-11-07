// Package share implements the share use case.
package share

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

// UseCase -.
type UseCase struct {
	shares  repo.ShareRepo
	folders repo.FolderRepo
	files   repo.FileRepo
	uow     repo.UnitOfWork
}

// New returns a Share use case. Its arguments are accepted in any order.
func New(args ...any) usecase.Share {
	uc := &UseCase{}

	for _, arg := range args {
		switch v := arg.(type) {
		case repo.ShareRepo:
			uc.shares = v
		case repo.FolderRepo:
			uc.folders = v
		case repo.FileRepo:
			uc.files = v
		case repo.UnitOfWork:
			uc.uow = v
		}
	}

	return uc
}

// Create makes a share for a folder. An active share for the same folder is
// reused and its expiry updated, so a folder never ends up with two live
// links; an expired link is revived the same way instead of leaving a dead
// row behind.
func (uc *UseCase) Create(
	ctx context.Context,
	userID, folderID string,
	expiresAt *time.Time,
) (entity.Share, error) {
	now := time.Now().UTC()

	var share entity.Share

	err := uc.uow.Do(ctx, func(ctx context.Context) error {
		if err := uc.folders.LockUser(ctx, userID); err != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", err)
		}

		if _, err := uc.folders.Get(ctx, userID, folderID); err != nil {
			return fmt.Errorf("uc.folders.Get: %w", err)
		}

		existing, found, err := uc.existingShare(ctx, userID, folderID)
		if err != nil {
			return err
		}

		if found {
			if err := uc.shares.UpdateExpiry(ctx, userID, existing.ID, expiresAt); err != nil {
				return fmt.Errorf("uc.shares.UpdateExpiry: %w", err)
			}

			existing.ExpiresAt = expiresAt
			existing.UpdatedAt = now
			share = existing

			return nil
		}

		share = entity.Share{
			ID:        uuid.New().String(),
			UserID:    userID,
			FolderID:  folderID,
			ExpiresAt: expiresAt,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := uc.shares.Store(ctx, &share); err != nil {
			return fmt.Errorf("uc.shares.Store: %w", err)
		}

		return nil
	})
	if err != nil {
		return entity.Share{}, fmt.Errorf("ShareUseCase - Create - uc.uow.Do: %w", err)
	}

	return share, nil
}

// List returns the active shares of an account.
func (uc *UseCase) List(ctx context.Context, userID string) ([]entity.Share, error) {
	shares, err := uc.shares.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ShareUseCase - List - uc.shares.ListByUser: %w", err)
	}

	return shares, nil
}

// Delete removes a share owned by the account.
func (uc *UseCase) Delete(ctx context.Context, userID, shareID string) error {
	if err := uc.shares.Delete(ctx, userID, shareID); err != nil {
		return fmt.Errorf("ShareUseCase - Delete - uc.shares.Delete: %w", err)
	}

	return nil
}

// UpdateExpiry changes the expiry of a share owned by the account.
func (uc *UseCase) UpdateExpiry(
	ctx context.Context,
	userID, shareID string,
	expiresAt *time.Time,
) (entity.Share, error) {
	if err := uc.shares.UpdateExpiry(ctx, userID, shareID, expiresAt); err != nil {
		return entity.Share{}, fmt.Errorf("ShareUseCase - UpdateExpiry - uc.shares.UpdateExpiry: %w", err)
	}

	share, err := uc.shares.Get(ctx, shareID)
	if err != nil {
		return entity.Share{}, fmt.Errorf("ShareUseCase - UpdateExpiry - uc.shares.Get: %w", err)
	}

	if share.UserID != userID {
		return entity.Share{}, entity.ErrShareNotFound
	}

	return share, nil
}

// Get returns a share by its token. An expired share is reported as expired.
func (uc *UseCase) Get(ctx context.Context, shareID string) (entity.Share, error) {
	share, err := uc.shares.Get(ctx, shareID)
	if err != nil {
		return entity.Share{}, err
	}

	if share.IsExpired(time.Now().UTC()) {
		return entity.Share{}, entity.ErrShareExpired
	}

	return share, nil
}

// ResolveFolder returns the share and the folder it opens. The requested
// folder must be the shared folder or one of its descendants, so a visitor
// cannot walk out of the shared tree by guessing an identifier.
func (uc *UseCase) ResolveFolder(
	ctx context.Context,
	shareID, folderID string,
) (entity.Share, entity.Folder, error) {
	share, err := uc.Get(ctx, shareID)
	if err != nil {
		return entity.Share{}, entity.Folder{}, err
	}

	if folderID == "" {
		folderID = share.FolderID
	}

	folder, err := uc.folders.Get(ctx, share.UserID, folderID)
	if err != nil {
		return entity.Share{}, entity.Folder{}, fmt.Errorf("ShareUseCase - ResolveFolder - uc.folders.Get: %w", err)
	}

	ok, err := folderInSubtree(ctx, uc.folders, share.UserID, share.FolderID, folderID)
	if err != nil {
		return entity.Share{}, entity.Folder{}, fmt.Errorf("ShareUseCase - ResolveFolder - folderInSubtree: %w", err)
	}

	if !ok {
		return entity.Share{}, entity.Folder{}, entity.ErrFolderNotFound
	}

	return share, folder, nil
}

// ResolveFile returns the share and the file it opens. The file must live in
// the shared folder or one of its descendants.
func (uc *UseCase) ResolveFile(
	ctx context.Context,
	shareID, fileID string,
) (entity.Share, entity.File, error) {
	share, err := uc.Get(ctx, shareID)
	if err != nil {
		return entity.Share{}, entity.File{}, err
	}

	file, err := uc.files.Get(ctx, share.UserID, fileID)
	if err != nil {
		return entity.Share{}, entity.File{}, fmt.Errorf("ShareUseCase - ResolveFile - uc.files.Get: %w", err)
	}

	ok, err := folderInSubtree(ctx, uc.folders, share.UserID, share.FolderID, file.FolderID)
	if err != nil {
		return entity.Share{}, entity.File{}, fmt.Errorf("ShareUseCase - ResolveFile - folderInSubtree: %w", err)
	}

	if !ok {
		return entity.Share{}, entity.File{}, entity.ErrFileNotFound
	}

	return share, file, nil
}

// existingShare returns a share row for the folder, active or expired, if one
// exists. It lets Create revive an expired link instead of inserting a
// duplicate.
func (uc *UseCase) existingShare(
	ctx context.Context,
	userID, folderID string,
) (entity.Share, bool, error) {
	existing, err := uc.shares.GetActiveByUserAndFolder(ctx, userID, folderID)
	if err == nil {
		return existing, true, nil
	}

	if !errors.Is(err, entity.ErrShareNotFound) {
		return entity.Share{}, false, fmt.Errorf("uc.shares.GetActiveByUserAndFolder: %w", err)
	}

	existing, err = uc.shares.GetByUserAndFolder(ctx, userID, folderID)
	if err == nil {
		return existing, true, nil
	}

	if !errors.Is(err, entity.ErrShareNotFound) {
		return entity.Share{}, false, fmt.Errorf("uc.shares.GetByUserAndFolder: %w", err)
	}

	return entity.Share{}, false, nil
}

func folderInSubtree(
	ctx context.Context,
	folders repo.FolderRepo,
	userID, rootID, folderID string,
) (bool, error) {
	if rootID == folderID {
		return true, nil
	}

	crumbs, err := folders.Breadcrumb(ctx, userID, folderID)
	if err != nil {
		return false, fmt.Errorf("FolderRepo - Breadcrumb: %w", err)
	}

	for i := range crumbs {
		if crumbs[i].ID == rootID {
			return true, nil
		}
	}

	return false, nil
}
