// Package folder implements the drive folder use case.
package folder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

// UseCase -.
type UseCase struct {
	folders repo.FolderRepo
	files   repo.FileRepo
	storage repo.ObjectStorage
	uow     repo.UnitOfWork
	bucket  string
}

// New returns a Folder use case.
func New(
	folders repo.FolderRepo,
	files repo.FileRepo,
	storage repo.ObjectStorage,
	uow repo.UnitOfWork,
	bucket string,
) usecase.Folder {
	return &UseCase{
		folders: folders,
		files:   files,
		storage: storage,
		uow:     uow,
		bucket:  bucket,
	}
}

// Get returns one folder of the account.
func (uc *UseCase) Get(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	folder, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Get - uc.folders.Get: %w", err)
	}

	return folder, nil
}

// List returns one folder of the account with the path that leads to it and
// the folders and files directly inside it. An empty identifier means the root
// folder, which is what the home page asks for.
func (uc *UseCase) List(ctx context.Context, userID, folderID string) (entity.FolderView, error) {
	folder, err := uc.read(ctx, userID, folderID)
	if err != nil {
		return entity.FolderView{}, err
	}

	breadcrumb, err := uc.folders.Breadcrumb(ctx, userID, folder.ID)
	if err != nil {
		return entity.FolderView{}, fmt.Errorf("FolderUseCase - List - uc.folders.Breadcrumb: %w", err)
	}

	folders, err := uc.folders.List(ctx, userID, folder.ID)
	if err != nil {
		return entity.FolderView{}, fmt.Errorf("FolderUseCase - List - uc.folders.List: %w", err)
	}

	files, err := uc.files.List(ctx, userID, folder.ID)
	if err != nil {
		return entity.FolderView{}, fmt.Errorf("FolderUseCase - List - uc.files.List: %w", err)
	}

	return entity.FolderView{
		Folder:     folder,
		Breadcrumb: breadcrumb,
		Folders:    folders,
		Files:      files,
	}, nil
}

// Create makes one folder inside another. The name has to be free in that
// folder, for a folder and for a file alike, because a listing shows both in
// one list and two entries with the same name would be indistinguishable. The
// parent row stays locked while the name is checked and the folder is stored,
// so a create and an upload cannot both claim the same name.
func (uc *UseCase) Create(
	ctx context.Context,
	userID, parentID, rawName string,
) (entity.Folder, error) {
	name, err := entity.CleanName(rawName)
	if err != nil {
		return entity.Folder{}, err
	}

	now := time.Now().UTC()

	folder := entity.Folder{
		ID:        uuid.New().String(),
		UserID:    userID,
		ParentID:  &parentID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		_, lockErr := uc.folders.Lock(ctx, userID, parentID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.Lock: %w", lockErr)
		}

		checkErr := uc.checkName(ctx, userID, parentID, name)
		if checkErr != nil {
			return checkErr
		}

		storeErr := uc.folders.Store(ctx, &folder)
		if storeErr != nil {
			return fmt.Errorf("uc.folders.Store: %w", storeErr)
		}

		return nil
	})
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Create - uc.uow.Do: %w", err)
	}

	return folder, nil
}

// Rename changes the name a folder is listed under. The root folder keeps its
// name, because the drive is reached through it, and the name has to be free
// in the parent for a folder and for a file alike. The account lock keeps a
// rename from racing a create or an upload into the same folder.
func (uc *UseCase) Rename(
	ctx context.Context,
	userID, folderID, rawName string,
) (entity.Folder, error) {
	name, err := entity.CleanName(rawName)
	if err != nil {
		return entity.Folder{}, err
	}

	folder, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Rename - uc.folders.Get: %w", err)
	}

	if folder.IsRoot() {
		return entity.Folder{}, entity.ErrRootFolder
	}

	parentID := *folder.ParentID
	oldName := folder.Name
	folder.Name = name
	folder.UpdatedAt = time.Now().UTC()

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		_, lockErr := uc.folders.Lock(ctx, userID, parentID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.Lock: %w", lockErr)
		}

		if !strings.EqualFold(oldName, name) {
			checkErr := uc.checkName(ctx, userID, parentID, name)
			if checkErr != nil {
				return checkErr
			}
		}

		renameErr := uc.folders.Rename(ctx, &folder)
		if renameErr != nil {
			return fmt.Errorf("uc.folders.Rename: %w", renameErr)
		}

		return nil
	})
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Rename - uc.uow.Do: %w", err)
	}

	return folder, nil
}

// Delete removes a folder and everything under it, and then removes the
// objects of the files it held. The rows go first, inside one transaction that
// also deletes the file rows and reads back their keys, so a file inserted
// while the delete runs is either captured or refused by the lock. An object
// left behind is invisible to the app, while a row without an object could
// still be listed. The parent folder comes back, so the caller knows where to
// send the visitor.
func (uc *UseCase) Delete(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	folder, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Delete - uc.folders.Get: %w", err)
	}

	if folder.IsRoot() {
		return entity.Folder{}, entity.ErrRootFolder
	}

	parent, err := uc.folders.Get(ctx, userID, *folder.ParentID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Delete - uc.folders.Get(parent): %w", err)
	}

	keys := make([]string, 0)

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		lockErr := uc.folders.LockSubtree(ctx, userID, folderID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.LockSubtree: %w", lockErr)
		}

		removedKeys, deleteErr := uc.files.DeleteByFolderSubtree(ctx, userID, folderID)
		if deleteErr != nil {
			return fmt.Errorf("uc.files.DeleteByFolderSubtree: %w", deleteErr)
		}

		keys = removedKeys

		folderErr := uc.folders.Delete(ctx, userID, folderID)
		if folderErr != nil {
			return fmt.Errorf("uc.folders.Delete: %w", folderErr)
		}

		return nil
	})
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Delete - uc.uow.Do: %w", err)
	}

	err = uc.removeAll(context.WithoutCancel(ctx), keys)
	if err != nil {
		return parent, err
	}

	return parent, nil
}

// read resolves a folder identifier, where an empty identifier is the root.
func (uc *UseCase) read(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	if folderID == "" {
		folder, err := uc.folders.GetRoot(ctx, userID)
		if err != nil {
			return entity.Folder{}, fmt.Errorf("FolderUseCase - read - uc.folders.GetRoot: %w", err)
		}

		return folder, nil
	}

	folder, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - read - uc.folders.Get: %w", err)
	}

	return folder, nil
}

// checkName rejects a name another entry of the folder already carries.
func (uc *UseCase) checkName(ctx context.Context, userID, folderID, name string) error {
	taken, err := uc.folders.NameTaken(ctx, userID, folderID, name)
	if err != nil {
		return fmt.Errorf("FolderUseCase - checkName - uc.folders.NameTaken: %w", err)
	}

	if taken {
		return entity.ErrEntryAlreadyExists
	}

	taken, err = uc.files.NameTaken(ctx, userID, folderID, name)
	if err != nil {
		return fmt.Errorf("FolderUseCase - checkName - uc.files.NameTaken: %w", err)
	}

	if taken {
		return entity.ErrEntryAlreadyExists
	}

	return nil
}

// removeAll deletes the objects of a removed subtree. A failure is reported
// after every key was tried, so one bad object does not keep the rest behind.
// The marker joins the error, so a caller can tell a successful delete with a
// failed cleanup from a delete that did not happen.
func (uc *UseCase) removeAll(ctx context.Context, keys []string) error {
	failures := make([]error, 0)

	for _, key := range keys {
		err := uc.storage.RemoveObject(ctx, uc.bucket, key)
		if err != nil {
			failures = append(failures, err)
		}
	}

	if len(failures) > 0 {
		failures = append(failures, entity.ErrStorageCleanup)

		return fmt.Errorf(
			"FolderUseCase - removeAll - uc.storage.RemoveObject: %w",
			errors.Join(failures...),
		)
	}

	return nil
}
