// Package usecase implements application business logic. Each logic group in own file.
package usecase

import (
	"context"
	"io"

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
		SetUserLimits(ctx context.Context, userID string, quota, uploadRate, downloadRate int64) (entity.User, error)
		UpdateUser(
			ctx context.Context,
			userID, username, email string,
			role entity.UserRole,
			password string,
		) (entity.User, bool, error)
	}

	// Folder is the drive's folder logic: listing one folder with its path,
	// creating a folder inside it, renaming it, and deleting a folder with
	// everything it holds.
	Folder interface {
		Get(ctx context.Context, userID, folderID string) (entity.Folder, error)
		List(ctx context.Context, userID, folderID string) (entity.FolderView, error)
		Create(ctx context.Context, userID, parentID, name string) (entity.Folder, error)
		Rename(ctx context.Context, userID, folderID, name string) (entity.Folder, error)
		Move(ctx context.Context, userID, folderID, destinationID string) (entity.Folder, error)
		Duplicate(ctx context.Context, userID, folderID, destinationID string) (entity.Folder, error)
		Delete(ctx context.Context, userID, folderID string) (entity.Folder, error)
		ListAll(ctx context.Context, userID string) ([]entity.Folder, error)
	}

	// File is the drive's file logic: one upload at a time, a rename, a
	// delete, and the link a browser downloads through.
	File interface {
		Get(ctx context.Context, userID, fileID string) (entity.File, error)
		Upload(
			ctx context.Context,
			userID, folderID, name, contentType string,
			size int64,
			body io.Reader,
		) (entity.File, error)
		Rename(ctx context.Context, userID, fileID, name string) (entity.File, error)
		Delete(ctx context.Context, userID, fileID string) (entity.File, error)
		Download(ctx context.Context, userID, fileID string) (string, error)
		Move(ctx context.Context, userID, fileID, folderID string) (entity.File, error)
		Duplicate(ctx context.Context, userID, fileID, folderID string) (entity.File, error)
	}
)
