// Package folder implements the drive folder use case.
package folder

import (
	"context"
	"errors"
	"fmt"
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

	// _maxDuplicateAttempts is how many numbered names a duplicate tries
	// before falling back to a timestamp.
	_maxDuplicateAttempts = 99
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

// ListAll returns every folder of the account. Roots come first, then children
// in name order. Used to build a destination picker for moves and uploads.
func (uc *UseCase) ListAll(ctx context.Context, userID string) ([]entity.Folder, error) {
	folders, err := uc.folders.ListAll(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("FolderUseCase - ListAll - uc.folders.ListAll: %w", err)
	}

	return folders, nil
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

// Move changes the parent of a folder. The root folder cannot move, a folder
// cannot move into itself or into one of its descendants, and the name has to
// be free in the destination for a folder and for a file alike. Only the row of
// the moved folder changes; everything under it stays attached. The account
// lock keeps the move from racing a create, an upload or a delete into either
// side.
func (uc *UseCase) Move(
	ctx context.Context,
	userID, folderID, destinationID string,
) (entity.Folder, error) {
	folder, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Move - uc.folders.Get: %w", err)
	}

	if folder.IsRoot() {
		return entity.Folder{}, entity.ErrRootFolder
	}

	if _, err := uc.folders.Get(ctx, userID, destinationID); err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Move - uc.folders.Get(destination): %w", err)
	}

	if folder.ParentID != nil && *folder.ParentID == destinationID {
		return folder, nil
	}

	return uc.moveFolder(ctx, userID, &folder, destinationID)
}

func (uc *UseCase) moveFolder(
	ctx context.Context,
	userID string,
	folder *entity.Folder,
	destinationID string,
) (entity.Folder, error) {
	err := uc.uow.Do(ctx, func(ctx context.Context) error {
		if err := uc.folders.LockUser(ctx, userID); err != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", err)
		}

		all, err := uc.folders.ListAll(ctx, userID)
		if err != nil {
			return fmt.Errorf("uc.folders.ListAll: %w", err)
		}

		if !folderExists(all, folder.ID) || !folderExists(all, destinationID) {
			return entity.ErrFolderNotFound
		}

		if folderIsDescendant(all, folder.ID, destinationID) {
			return entity.ErrFolderCycle
		}

		if err := uc.checkName(ctx, userID, destinationID, folder.Name); err != nil {
			return err
		}

		now := time.Now().UTC()
		folder.ParentID = &destinationID
		folder.UpdatedAt = now

		if err := uc.folders.Move(ctx, userID, folder.ID, destinationID); err != nil {
			return fmt.Errorf("uc.folders.Move: %w", err)
		}

		return nil
	})
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Move - uc.uow.Do: %w", err)
	}

	return *folder, nil
}

// Duplicate copies a folder and everything under it into another folder. The
// new top-level folder gets a free name in the destination, with ` (1)` and
// so on appended when the original name is taken, like a file duplicate. The
// folder is copied by creating new rows for every folder and file in the
// subtree and server-side copying each object, so both copies exist
// independently. The destination folder is locked while the rows are written,
// so a duplicate and an upload cannot both claim one name.
//
//nolint:gocognit,gocyclo,cyclop,funlen // recursive subtree copy needs sequential validation and transaction handling
func (uc *UseCase) Duplicate(
	ctx context.Context,
	userID, folderID, destinationID string,
) (entity.Folder, error) {
	source, err := uc.folders.Get(ctx, userID, folderID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.folders.Get: %w", err)
	}

	if source.IsRoot() {
		return entity.Folder{}, entity.ErrRootFolder
	}

	if _, err := uc.folders.Get(ctx, userID, destinationID); err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.folders.Get(destination): %w", err)
	}

	all, err := uc.folders.ListAll(ctx, userID)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.folders.ListAll: %w", err)
	}

	if !folderExists(all, source.ID) || !folderExists(all, destinationID) {
		return entity.Folder{}, entity.ErrFolderNotFound
	}

	if folderIsDescendant(all, source.ID, destinationID) {
		return entity.Folder{}, entity.ErrFolderCycle
	}

	sourceIDs := collectFolderSubtree(all, source.ID)

	filesToCopy := make([]struct {
		file     entity.File
		folderID string
	}, 0)

	for _, oldFolderID := range sourceIDs {
		items, err := uc.files.List(ctx, userID, oldFolderID)
		if err != nil {
			return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.files.List: %w", err)
		}

		for i := range items {
			file, err := uc.files.Get(ctx, userID, items[i].ID)
			if err != nil {
				return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.files.Get: %w", err)
			}

			filesToCopy = append(filesToCopy, struct {
				file     entity.File
				folderID string
			}{file: file, folderID: oldFolderID})
		}
	}

	now := time.Now().UTC()

	name, err := uc.duplicateName(ctx, userID, destinationID, source.Name, now)
	if err != nil {
		return entity.Folder{}, err
	}

	idMap := make(map[string]string, len(sourceIDs))
	for _, oldID := range sourceIDs {
		idMap[oldID] = uuid.New().String()
	}

	var (
		newTop     entity.Folder
		copiedKeys []string
	)

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		if err := uc.folders.LockUser(ctx, userID); err != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", err)
		}

		if _, err := uc.folders.Lock(ctx, userID, destinationID); err != nil {
			return fmt.Errorf("uc.folders.Lock: %w", err)
		}

		taken, err := uc.folders.NameTaken(ctx, userID, destinationID, name)
		if err != nil {
			return fmt.Errorf("uc.folders.NameTaken: %w", err)
		}

		if taken {
			fileTaken, err := uc.files.NameTaken(ctx, userID, destinationID, name)
			if err != nil {
				return fmt.Errorf("uc.files.NameTaken: %w", err)
			}

			if fileTaken {
				return entity.ErrEntryAlreadyExists
			}
		}

		for _, oldID := range sourceIDs {
			oldFolder, ok := folderByID(all, oldID)
			if !ok {
				return entity.ErrFolderNotFound
			}

			parentID := destinationID
			if oldID != source.ID {
				parentID = idMap[*oldFolder.ParentID]
			}

			newFolder := entity.Folder{
				ID:        idMap[oldID],
				UserID:    userID,
				ParentID:  &parentID,
				Name:      oldFolder.Name,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if oldID == source.ID {
				newFolder.Name = name
			}

			if err := uc.folders.Store(ctx, &newFolder); err != nil {
				return fmt.Errorf("uc.folders.Store: %w", err)
			}

			if oldID == source.ID {
				newTop = newFolder
			}
		}

		for i := range filesToCopy {
			fc := &filesToCopy[i]

			newID := uuid.New().String()
			newKey := storageKey(userID, newID)

			if err := uc.storage.CopyObject(ctx, uc.bucket, newKey, fc.file.StorageKey); err != nil {
				return fmt.Errorf("uc.storage.CopyObject: %w", err)
			}

			copiedKeys = append(copiedKeys, newKey)

			dupFile := entity.File{
				ID:          newID,
				UserID:      userID,
				FolderID:    idMap[fc.folderID],
				Name:        fc.file.Name,
				ContentType: fc.file.ContentType,
				StorageKey:  newKey,
				Size:        fc.file.Size,
				CreatedAt:   now,
				UpdatedAt:   now,
			}

			if err := uc.files.Store(ctx, &dupFile); err != nil {
				if removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, newKey); removeErr != nil {
					return fmt.Errorf("uc.files.Store: %w; cleanup: %w", err, removeErr)
				}

				copiedKeys = copiedKeys[:len(copiedKeys)-1]

				return fmt.Errorf("uc.files.Store: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		for _, key := range copiedKeys {
			if removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, key); removeErr != nil {
				_ = removeErr
			}
		}

		return entity.Folder{}, fmt.Errorf("FolderUseCase - Duplicate - uc.uow.Do: %w", err)
	}

	return newTop, nil
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
		return "", fmt.Errorf("FolderUseCase - duplicateName - uc.nameTakenInFolder: %w", err)
	}

	if !taken {
		return name, nil
	}

	base, ext := splitFolderName(name)

	for i := 1; i <= _maxDuplicateAttempts; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if utf8.RuneCountInString(candidate) > _maxNameLength {
			continue
		}

		taken, err := uc.nameTakenInFolder(ctx, userID, folderID, candidate)
		if err != nil {
			return "", fmt.Errorf("FolderUseCase - duplicateName - uc.nameTakenInFolder: %w", err)
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

// collectFolderSubtree returns the source folder and every folder under it, in
// an order where a parent always comes before its children.
func collectFolderSubtree(folders []entity.Folder, rootID string) []string {
	children := make(map[string][]string, len(folders))
	for i := range folders {
		f := folders[i]
		if f.ParentID != nil {
			children[*f.ParentID] = append(children[*f.ParentID], f.ID)
		}
	}

	result := make([]string, 0)

	var visit func(id string)

	visit = func(id string) {
		result = append(result, id)
		for _, child := range children[id] {
			visit(child)
		}
	}

	visit(rootID)

	return result
}

// folderByID returns the folder with the given identifier from a flat list.
func folderByID(folders []entity.Folder, id string) (entity.Folder, bool) {
	for i := range folders {
		if folders[i].ID == id {
			return folders[i], true
		}
	}

	return entity.Folder{}, false
}

// splitFolderName splits a name into the base and the extension including the
// dot. A name without an extension gets an empty extension.
func splitFolderName(name string) (base, ext string) {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[:i], name[i:]
	}

	return name, ""
}

// storageKey builds the object key of a file. It holds identifiers only, so a
// rename never has to move or copy the stored object.
func storageKey(userID, fileID string) string {
	return "users/" + userID + "/files/" + fileID
}

func folderExists(folders []entity.Folder, id string) bool {
	for i := range folders {
		if folders[i].ID == id {
			return true
		}
	}

	return false
}

func folderIsDescendant(folders []entity.Folder, folderID, destinationID string) bool {
	parentOf := make(map[string]*string, len(folders))
	for i := range folders {
		f := folders[i]
		parentOf[f.ID] = f.ParentID
	}

	current := destinationID
	seen := make(map[string]bool)

	for current != "" {
		if current == folderID {
			return true
		}

		if seen[current] {
			return false
		}

		seen[current] = true

		parent, ok := parentOf[current]
		if !ok {
			return false
		}

		if parent == nil {
			return false
		}

		current = *parent
	}

	return false
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
