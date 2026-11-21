// Package usecase implements application business logic. Each logic group in own file.
package usecase

import (
	"context"
	"io"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

type (
	// User -.
	User interface {
		Register(ctx context.Context, username, email, password string) (entity.User, error)
		Login(ctx context.Context, email, password string) (string, entity.User, error)
		Authenticate(ctx context.Context, sessionID string) (entity.Session, error)
		Logout(ctx context.Context, sessionID string) error
		GetUser(ctx context.Context, userID string) (entity.User, error)
		ListUsers(ctx context.Context, filter *entity.UserFilter) (entity.UserList, error)
		SetRole(ctx context.Context, email string, role entity.UserRole) error
		SetStatus(ctx context.Context, userID string, status entity.UserStatus) error
		ApproveUser(ctx context.Context, userID string) error
		DisableUser(ctx context.Context, userID string) error
		EnableUser(ctx context.Context, userID string) error
		UpdateStatus(ctx context.Context, userID string, status entity.UserStatus) error
		SetUserLimits(ctx context.Context, userID string, quota, uploadRate, downloadRate int64) (entity.User, error)
		UpdateUser(
			ctx context.Context,
			userID, username, email string,
			role entity.UserRole,
			password string,
		) (entity.User, bool, error)
	}

	// Folder is the drive's folder logic: listing one folder with its path,
	// searching the drive by name, creating a folder inside it, renaming it,
	// and deleting a folder with everything it holds.
	Folder interface {
		Get(ctx context.Context, userID, folderID string) (entity.Folder, error)
		List(
			ctx context.Context,
			userID, folderID, search string,
			after *entity.EntryCursor,
		) (entity.FolderView, error)
		Search(
			ctx context.Context,
			userID, search, from string,
			after *entity.EntryCursor,
		) (entity.SearchView, error)
		Create(ctx context.Context, userID, parentID, name string) (entity.Folder, error)
		Rename(ctx context.Context, userID, folderID, name string) (entity.Folder, error)
		Move(ctx context.Context, userID, folderID, destinationID string) (entity.Folder, error)
		Duplicate(ctx context.Context, userID, folderID, destinationID string) (entity.Folder, error)
		Delete(ctx context.Context, userID, folderID string) (entity.Folder, error)
		ListAll(ctx context.Context, userID string) ([]entity.Folder, error)
		DownloadZip(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error)
		DownloadFolder(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error)
		ZipFolder(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error)
		FolderZip(ctx context.Context, userID, folderID string) (io.ReadCloser, string, error)
	}

	// Share is the public-link logic for folders: create a link with an
	// expiry, list and revoke the links of an account, and resolve a link
	// to the folder (and anything inside it) it opens.
	Share interface {
		Create(ctx context.Context, userID, folderID string, expiresAt *time.Time) (entity.Share, error)
		List(ctx context.Context, userID string) ([]entity.Share, error)
		Delete(ctx context.Context, userID, shareID string) error
		UpdateExpiry(ctx context.Context, userID, shareID string, expiresAt *time.Time) (entity.Share, error)
		Get(ctx context.Context, shareID string) (entity.Share, error)
		ResolveFolder(ctx context.Context, shareID, folderID string) (entity.Share, entity.Folder, error)
		ResolveFile(ctx context.Context, shareID, fileID string) (entity.Share, entity.File, error)
	}

	// File is the drive's file logic: a chunked upload split over several
	// requests, a rename, a delete, and the link a browser downloads through.
	File interface {
		Get(ctx context.Context, userID, fileID string) (entity.File, error)
		Rename(ctx context.Context, userID, fileID, name string) (entity.File, error)
		Delete(ctx context.Context, userID, fileID string) (entity.File, error)
		Download(ctx context.Context, userID, fileID string) (string, error)
		OpenDownload(ctx context.Context, userID, fileID string) (io.ReadCloser, entity.File, error)
		DownloadFile(ctx context.Context, userID, fileID string) (io.ReadCloser, entity.File, error)
		DownloadStream(ctx context.Context, userID, fileID string) (io.ReadCloser, entity.File, error)
		OpenFile(ctx context.Context, userID, fileID string) (io.ReadCloser, entity.File, error)
		Move(ctx context.Context, userID, fileID, folderID string) (entity.File, error)
		Duplicate(ctx context.Context, userID, fileID, folderID string) (entity.File, error)
		StartUpload(
			ctx context.Context,
			userID, folderID, name, contentType string,
			size int64,
		) (entity.UploadSession, error)
		UploadPart(
			ctx context.Context,
			userID, uploadID string,
			partNumber int,
			body io.Reader,
			size int64,
		) error
		CompleteUpload(ctx context.Context, userID, uploadID string) (entity.File, error)
		AbortUpload(ctx context.Context, userID, uploadID string) error
		SweepUploads(ctx context.Context) (int, error)
	}
)

// Limits carries the global defaults a per-account zero inherits.
// Limits carries the account-wide defaults. An account inherits a default
// quota, upload rate or download rate when it carries no value of its own.
type Limits struct {
	StorageQuotaBytes       int64
	DownloadRateBytesPerSec int64
}

// UploadSettings carries the size and pacing limits of a chunked upload. A
// zero MaxBytes, PartBytes or SessionIdleTTL falls back to the use case
// default, and an account inherits the default upload rate when it carries no
// value of its own.
type UploadSettings struct {
	MaxBytes                     int64
	PartBytes                    int64
	SessionIdleTTL               time.Duration
	DefaultUploadRateBytesPerSec int64
}
