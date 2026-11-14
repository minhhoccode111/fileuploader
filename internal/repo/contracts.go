// Package repo implements application outer layer logic. Each logic group in own file.
package repo

import (
	"context"
	"io"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

type (
	// UnitOfWork runs a group of repository calls inside a single database
	// transaction. The context it hands to the callback carries the
	// transaction, so every repository called with it writes inside the same
	// transaction. The transaction is committed when the callback returns nil
	// and rolled back when it returns an error.
	UnitOfWork interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	// UserRepo -.
	UserRepo interface {
		Store(ctx context.Context, user *entity.User) error
		GetByID(ctx context.Context, id string) (entity.User, error)
		GetByEmail(ctx context.Context, email string) (entity.User, error)
		List(ctx context.Context, filter *entity.UserFilter) (entity.UserList, error)
		UpdateRole(ctx context.Context, userID string, role entity.UserRole) error
		Update(ctx context.Context, user *entity.User) error
		AdjustStorageUsed(ctx context.Context, userID string, delta int64) error
		SetUserLimits(ctx context.Context, userID string, quota, uploadRate, downloadRate int64) error
	}

	// SessionRepo -.
	SessionRepo interface {
		Store(ctx context.Context, id string, session entity.Session, ttl time.Duration) error
		Get(ctx context.Context, id string) (entity.Session, error)
		Delete(ctx context.Context, id string) error
		DeleteUserSessions(ctx context.Context, userID string) error
	}

	// FolderRepo -.
	FolderRepo interface {
		Store(ctx context.Context, folder *entity.Folder) error
		Get(ctx context.Context, userID, folderID string) (entity.Folder, error)
		LockUser(ctx context.Context, userID string) error
		Lock(ctx context.Context, userID, folderID string) (entity.Folder, error)
		LockSubtree(ctx context.Context, userID, folderID string) error
		GetRoot(ctx context.Context, userID string) (entity.Folder, error)
		List(ctx context.Context, userID, folderID string) ([]entity.FolderItem, error)
		ListAll(ctx context.Context, userID string) ([]entity.Folder, error)
		Breadcrumb(ctx context.Context, userID, folderID string) ([]entity.FolderRef, error)
		NameTaken(ctx context.Context, userID, parentID, name string) (bool, error)
		Rename(ctx context.Context, folder *entity.Folder) error
		Move(ctx context.Context, userID, folderID, parentID string) error
		Delete(ctx context.Context, userID, folderID string) error
	}

	// FileRepo -.
	FileRepo interface {
		Store(ctx context.Context, file *entity.File) error
		Get(ctx context.Context, userID, fileID string) (entity.File, error)
		List(ctx context.Context, userID, folderID string) ([]entity.FileItem, error)
		Rename(ctx context.Context, file *entity.File) error
		Move(ctx context.Context, userID, fileID, folderID string) error
		Delete(ctx context.Context, userID, fileID string) (entity.File, error)
		NameTaken(ctx context.Context, userID, folderID, name string) (bool, error)
		DeleteByFolderSubtree(ctx context.Context, userID, folderID string) ([]entity.File, error)
	}

	// ShareRepo stores the public links an account creates to its folders.
	ShareRepo interface {
		Store(ctx context.Context, share *entity.Share) error
		Get(ctx context.Context, shareID string) (entity.Share, error)
		GetActiveByUserAndFolder(ctx context.Context, userID, folderID string) (entity.Share, error)
		GetByUserAndFolder(ctx context.Context, userID, folderID string) (entity.Share, error)
		ListByUser(ctx context.Context, userID string) ([]entity.Share, error)
		UpdateExpiry(ctx context.Context, userID, shareID string, expiresAt *time.Time) error
		Delete(ctx context.Context, userID, shareID string) error
	}

	// ObjectStorage is the bucket the drive keeps its bytes in. The key is the
	// only thing the repository layer knows about it, so the storage can be
	// swapped without touching the domain.
	ObjectStorage interface {
		PutObject(
			ctx context.Context,
			bucket, key string,
			reader io.Reader,
			size int64,
			contentType string,
		) error
		PresignedGetObject(
			ctx context.Context,
			bucket, key string,
			expiry time.Duration,
			downloadName string,
		) (string, error)
		RemoveObject(ctx context.Context, bucket, key string) error
		CopyObject(ctx context.Context, bucket, dstKey, srcKey string) error
		GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
		CreateMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error)
		UploadPart(
			ctx context.Context,
			bucket, key, uploadID string,
			partNumber int,
			reader io.Reader,
			size int64,
		) (string, error)
		CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []multipart.Part) error
		AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	}
)
