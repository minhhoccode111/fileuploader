// Package folder implements the drive folder use case.
package folder

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
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
// _zipEntry is one file inside a streamed zip archive.
type _zipEntry struct {
	path string
	file entity.File
}

type UseCase struct {
	users        repo.UserRepo
	folders      repo.FolderRepo
	files        repo.FileRepo
	storage      repo.ObjectStorage
	uow          repo.UnitOfWork
	bucket       string
	defaultQuota int64
}

// New returns a Folder use case. Its arguments are accepted in any order, so the
// user repository can be added without breaking existing callers.
func New(args ...any) usecase.Folder {
	uc := &UseCase{}

	for _, arg := range args {
		switch v := arg.(type) {
		case repo.UserRepo:
			uc.users = v
		case repo.FolderRepo:
			uc.folders = v
		case repo.FileRepo:
			uc.files = v
		case repo.ObjectStorage:
			uc.storage = v
		case repo.UnitOfWork:
			uc.uow = v
		case string:
			if uc.bucket == "" {
				uc.bucket = v
			}
		case usecase.Limits:
			uc.defaultQuota = v.StorageQuotaBytes
		}
	}

	return uc
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

		all, listErr := uc.folders.ListAll(ctx, userID)
		if listErr != nil {
			return fmt.Errorf("uc.folders.ListAll: %w", listErr)
		}

		if entity.FolderDepth(all, parentID)+1 > entity.MaxFolderDepth {
			return entity.ErrFolderTooDeep
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

		if err := validateMove(all, folder, destinationID); err != nil {
			return err
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

	now := time.Now().UTC()

	var (
		newTop     entity.Folder
		copiedKeys []string
	)

	var (
		all         []entity.Folder
		sourceIDs   []string
		idMap       map[string]string
		filesToCopy []struct {
			file     entity.File
			folderID string
		}
	)

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		if err := uc.folders.LockUser(ctx, userID); err != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", err)
		}

		if err := uc.folders.LockSubtree(ctx, userID, source.ID); err != nil {
			return fmt.Errorf("uc.folders.LockSubtree: %w", err)
		}

		if _, err := uc.folders.Lock(ctx, userID, destinationID); err != nil {
			return fmt.Errorf("uc.folders.Lock: %w", err)
		}

		all, err = uc.folders.ListAll(ctx, userID)
		if err != nil {
			return fmt.Errorf("uc.folders.ListAll: %w", err)
		}

		if !folderExists(all, source.ID) || !folderExists(all, destinationID) {
			return entity.ErrFolderNotFound
		}

		if folderIsDescendant(all, source.ID, destinationID) {
			return entity.ErrFolderCycle
		}

		if exceedsMaxDepth(all, source.ID, destinationID) {
			return entity.ErrFolderTooDeep
		}

		freshSource, ok := folderByID(all, source.ID)
		if !ok {
			return entity.ErrFolderNotFound
		}

		source = freshSource

		sourceIDs = collectFolderSubtree(all, source.ID)

		filesToCopy = make([]struct {
			file     entity.File
			folderID string
		}, 0)

		for _, oldFolderID := range sourceIDs {
			items, err := uc.files.List(ctx, userID, oldFolderID)
			if err != nil {
				return fmt.Errorf("uc.files.List: %w", err)
			}

			for i := range items {
				file, err := uc.files.Get(ctx, userID, items[i].ID)
				if err != nil {
					return fmt.Errorf("uc.files.Get: %w", err)
				}

				filesToCopy = append(filesToCopy, struct {
					file     entity.File
					folderID string
				}{file: file, folderID: oldFolderID})
			}
		}

		var totalSize int64

		for i := range filesToCopy {
			totalSize += filesToCopy[i].file.Size
		}

		if uc.users != nil {
			user, userErr := uc.users.GetByID(ctx, userID)
			if userErr != nil {
				return fmt.Errorf("FolderUseCase - Duplicate - uc.users.GetByID: %w", userErr)
			}

			quota := user.EffectiveStorageQuota(uc.defaultQuota)
			if quota > 0 && user.StorageUsedBytes+totalSize > quota {
				return entity.ErrStorageQuotaExceeded
			}
		}

		idMap = make(map[string]string, len(sourceIDs))
		for _, oldID := range sourceIDs {
			idMap[oldID] = uuid.New().String()
		}

		name, err := uc.duplicateName(ctx, userID, destinationID, source.Name, now)
		if err != nil {
			return err
		}

		taken, err := uc.folders.NameTaken(ctx, userID, destinationID, name)
		if err != nil {
			return fmt.Errorf("uc.folders.NameTaken: %w", err)
		}

		fileTaken, err := uc.files.NameTaken(ctx, userID, destinationID, name)
		if err != nil {
			return fmt.Errorf("uc.files.NameTaken: %w", err)
		}

		if taken || fileTaken {
			return entity.ErrEntryAlreadyExists
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

		if uc.users != nil {
			if err := uc.adjustStorageUsage(ctx, userID, totalSize); err != nil {
				return fmt.Errorf("uc.users.AdjustStorageUsage: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		cleanupErrs := make([]error, 0, len(copiedKeys))

		for _, key := range copiedKeys {
			if removeErr := uc.storage.RemoveObject(context.WithoutCancel(ctx), uc.bucket, key); removeErr != nil {
				cleanupErrs = append(cleanupErrs, removeErr)
			}
		}

		if len(cleanupErrs) > 0 {
			cleanupErrs = append(cleanupErrs, entity.ErrStorageCleanup)

			return entity.Folder{}, fmt.Errorf(
				"FolderUseCase - Duplicate - uc.uow.Do: %w; cleanup: %w",
				err,
				errors.Join(cleanupErrs...),
			)
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

	taken, err = uc.nameTakenInFolder(ctx, userID, folderID, candidate)
	if err != nil {
		return "", fmt.Errorf("FolderUseCase - duplicateName - uc.nameTakenInFolder: %w", err)
	}

	if taken {
		return "", entity.ErrEntryAlreadyExists
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

// DownloadZip streams a zip archive of the folder and everything under it.
// The returned reader is the archive; the caller closes it. The name is the
// file name the browser should save the archive under.
func (uc *UseCase) DownloadZip(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error) {
	entries, root, err := uc.collectZipEntries(ctx, userID, folderID)
	if err != nil {
		return nil, "", err
	}

	name := root.Name
	if name == "/" || name == "" {
		name = "root"
	}

	filename := name + ".zip"

	reader, writer := io.Pipe()

	go uc.writeZip(ctx, writer, entries)

	return reader, filename, nil
}

// collectZipEntries gathers the files that make up the archive.
func (uc *UseCase) collectZipEntries(
	ctx context.Context,
	userID, folderID string,
) ([]_zipEntry, entity.Folder, error) {
	root, err := uc.read(ctx, userID, folderID)
	if err != nil {
		return nil, entity.Folder{}, fmt.Errorf("FolderUseCase - DownloadZip - uc.read: %w", err)
	}

	all, err := uc.folders.ListAll(ctx, userID)
	if err != nil {
		return nil, entity.Folder{}, fmt.Errorf("FolderUseCase - DownloadZip - uc.folders.ListAll: %w", err)
	}

	if !folderExists(all, root.ID) {
		return nil, entity.Folder{}, entity.ErrFolderNotFound
	}

	relative := uc.relativePaths(all, root.ID)

	subtree := collectFolderSubtree(all, root.ID)

	entries := make([]_zipEntry, 0)

	for _, id := range subtree {
		collected, err := uc.collectFolderEntries(ctx, userID, id, relative[id])
		if err != nil {
			return nil, entity.Folder{}, err
		}

		entries = append(entries, collected...)
	}

	return entries, root, nil
}

// collectFolderEntries gathers the files of one folder for the archive.
func (uc *UseCase) collectFolderEntries(
	ctx context.Context,
	userID, folderID, prefix string,
) ([]_zipEntry, error) {
	items, err := uc.files.List(ctx, userID, folderID)
	if err != nil {
		return nil, fmt.Errorf("FolderUseCase - DownloadZip - uc.files.List: %w", err)
	}

	entries := make([]_zipEntry, 0, len(items))

	for i := range items {
		file, getErr := uc.files.Get(ctx, userID, items[i].ID)
		if getErr != nil {
			return nil, fmt.Errorf("FolderUseCase - DownloadZip - uc.files.Get: %w", getErr)
		}

		probe, probeErr := uc.storage.GetObject(ctx, uc.bucket, file.StorageKey)
		if probeErr != nil {
			return nil, fmt.Errorf("FolderUseCase - DownloadZip - probe: %w", probeErr)
		}

		_ = probe.Close()

		path := items[i].Name
		if prefix != "" {
			path = prefix + "/" + items[i].Name
		}

		entries = append(entries, _zipEntry{path: path, file: file})
	}

	return entries, nil
}

// relativePaths maps each folder in the subtree to its path inside the archive.
func (uc *UseCase) relativePaths(all []entity.Folder, rootID string) map[string]string {
	relative := map[string]string{rootID: ""}

	var walk func(id, prefix string)

	walk = func(id, prefix string) {
		for i := range all {
			f := all[i]
			if f.ParentID == nil || *f.ParentID != id {
				continue
			}

			childPrefix := f.Name
			if prefix != "" {
				childPrefix = prefix + "/" + f.Name
			}

			relative[f.ID] = childPrefix
			walk(f.ID, childPrefix)
		}
	}

	walk(rootID, "")

	return relative
}

// writeZip writes the archive to the pipe writer.
func (uc *UseCase) writeZip(ctx context.Context, writer *io.PipeWriter, entries []_zipEntry) {
	zw := zip.NewWriter(writer)
	defer zw.Close()

	for i := range entries {
		e := entries[i]

		header := &zip.FileHeader{
			Name:     e.path,
			Method:   zip.Deflate,
			Modified: e.file.UpdatedAt,
		}

		w, err := zw.CreateHeader(header)
		if err != nil {
			writer.CloseWithError(fmt.Errorf("FolderUseCase - DownloadZip - CreateHeader: %w", err))

			return
		}

		object, err := uc.storage.GetObject(ctx, uc.bucket, e.file.StorageKey)
		if err != nil {
			writer.CloseWithError(fmt.Errorf("FolderUseCase - DownloadZip - GetObject: %w", err))

			return
		}

		if _, err := io.Copy(w, object); err != nil {
			_ = object.Close()

			writer.CloseWithError(fmt.Errorf("FolderUseCase - DownloadZip - Copy: %w", err))

			return
		}

		if err := object.Close(); err != nil {
			writer.CloseWithError(fmt.Errorf("FolderUseCase - DownloadZip - Close: %w", err))

			return
		}
	}

	if err := zw.Close(); err != nil {
		writer.CloseWithError(fmt.Errorf("FolderUseCase - DownloadZip - CloseZip: %w", err))

		return
	}

	_ = writer.Close()
}

// DownloadFolder is an alias for DownloadZip.
func (uc *UseCase) DownloadFolder(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error) {
	return uc.DownloadZip(ctx, userID, folderID)
}

// ZipFolder is an alias for DownloadZip.
func (uc *UseCase) ZipFolder(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error) {
	return uc.DownloadZip(ctx, userID, folderID)
}

// FolderZip is an alias for DownloadZip.
func (uc *UseCase) FolderZip(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error) {
	return uc.DownloadZip(ctx, userID, folderID)
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

// validateMove checks that the move is allowed: both folders exist, the
// destination is not the source or one of its descendants, and the move would
// not push the subtree past the deepest level the drive allows.
func validateMove(all []entity.Folder, folder *entity.Folder, destinationID string) error {
	if !folderExists(all, folder.ID) || !folderExists(all, destinationID) {
		return entity.ErrFolderNotFound
	}

	if folderIsDescendant(all, folder.ID, destinationID) {
		return entity.ErrFolderCycle
	}

	if exceedsMaxDepth(all, folder.ID, destinationID) {
		return entity.ErrFolderTooDeep
	}

	return nil
}

// exceedsMaxDepth reports whether moving or copying the folder with folderID into
// destinationID would push the subtree past the deepest level the drive allows.
func exceedsMaxDepth(folders []entity.Folder, folderID, destinationID string) bool {
	return entity.FolderDepth(folders, destinationID)+1+
		entity.FolderSubtreeHeight(folders, folderID) > entity.MaxFolderDepth
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
//
//nolint:gocognit,gocyclo,cyclop // quota accounting adds branches
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

	var freedSize int64

	err = uc.uow.Do(ctx, func(ctx context.Context) error {
		userLockErr := uc.folders.LockUser(ctx, userID)
		if userLockErr != nil {
			return fmt.Errorf("uc.folders.LockUser: %w", userLockErr)
		}

		lockErr := uc.folders.LockSubtree(ctx, userID, folderID)
		if lockErr != nil {
			return fmt.Errorf("uc.folders.LockSubtree: %w", lockErr)
		}

		removedFiles, deleteErr := uc.files.DeleteByFolderSubtree(ctx, userID, folderID)
		if deleteErr != nil {
			return fmt.Errorf("uc.files.DeleteByFolderSubtree: %w", deleteErr)
		}

		for i := range removedFiles {
			keys = append(keys, removedFiles[i].StorageKey)
			freedSize += removedFiles[i].Size
		}

		folderErr := uc.folders.Delete(ctx, userID, folderID)
		if folderErr != nil {
			return fmt.Errorf("uc.folders.Delete: %w", folderErr)
		}

		if uc.users != nil {
			if err := uc.adjustStorageUsage(ctx, userID, -freedSize); err != nil {
				return fmt.Errorf("uc.users.AdjustStorageUsage: %w", err)
			}
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

// adjustStorageUsage applies a signed byte delta to an account's stored usage.
func (uc *UseCase) adjustStorageUsage(ctx context.Context, userID string, delta int64) error {
	if uc.users == nil {
		return nil
	}

	return uc.users.AdjustStorageUsed(ctx, userID, delta)
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
