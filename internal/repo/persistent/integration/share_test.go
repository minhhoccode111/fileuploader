//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	shareRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/share"
)

func TestShareStoreAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("share"), uniqueName("share")+"@example.com")
	root := seedRoot(t, owner.ID)
	expires := time.Now().UTC().Add(time.Hour)
	share := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  root.ID,
		ExpiresAt: &expires,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	repo := shareRepo.New(testPostgres(t))
	if err := repo.Store(context.Background(), &share); err != nil {
		t.Fatalf("Store() error = %v, want nil", err)
	}

	got, err := repo.Get(context.Background(), share.ID)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if got.UserID != share.UserID || got.FolderID != share.FolderID || got.ExpiresAt == nil {
		t.Errorf("Get() = %+v, want the stored share", got)
	}
}

func TestActiveShareFilter(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("active"), uniqueName("active")+"@example.com")
	root := seedRoot(t, owner.ID)
	activeFolder := seedFolder(t, owner.ID, root.ID, "Active")
	expiredFolder := seedFolder(t, owner.ID, root.ID, "Expired")
	expired := time.Now().UTC().Add(-time.Hour)
	active := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  activeFolder.ID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	old := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  expiredFolder.ID,
		ExpiresAt: &expired,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	repo := shareRepo.New(testPostgres(t))
	if err := repo.Store(context.Background(), &active); err != nil {
		t.Fatalf("Store(active) error = %v, want nil", err)
	}

	if err := repo.Store(context.Background(), &old); err != nil {
		t.Fatalf("Store(expired) error = %v, want nil", err)
	}

	other := seedUser(t, uniqueName("other-active"), uniqueName("other-active")+"@example.com")
	otherRoot := seedRoot(t, other.ID)
	otherFolder := seedFolder(t, other.ID, otherRoot.ID, "Other")
	otherExpiry := time.Now().UTC().Add(time.Hour)
	otherShare := entity.Share{
		ID:        uuid.NewString(),
		UserID:    other.ID,
		FolderID:  otherFolder.ID,
		ExpiresAt: &otherExpiry,
		CreatedAt: time.Now().UTC().Add(time.Minute),
		UpdatedAt: time.Now().UTC().Add(time.Minute),
	}

	if err := repo.Store(context.Background(), &otherShare); err != nil {
		t.Fatalf("Store(other) error = %v, want nil", err)
	}

	got, err := repo.GetActiveByUserAndFolder(context.Background(), owner.ID, activeFolder.ID)
	if err != nil {
		t.Fatalf("GetActiveByUserAndFolder() error = %v, want nil", err)
	}

	if got.ID != active.ID {
		t.Errorf("active share = %q, want %q, not another account's share", got.ID, active.ID)
	}

	list, err := repo.ListByUser(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("ListByUser() error = %v, want nil", err)
	}

	if len(list) != 1 || list[0].ID != active.ID {
		t.Errorf("ListByUser() = %+v, want only the owner's active share", list)
	}

	oldGot, err := repo.GetByUserAndFolder(context.Background(), owner.ID, expiredFolder.ID)
	if err != nil {
		t.Fatalf("GetByUserAndFolder() error = %v, want nil", err)
	}

	if oldGot.ID != old.ID {
		t.Errorf("GetByUserAndFolder() = %q, want the expired share %q", oldGot.ID, old.ID)
	}
}

func TestShareUpdateAndDeleteScopeToOwner(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("share-owner"), uniqueName("share-owner")+"@example.com")
	other := seedUser(t, uniqueName("share-other"), uniqueName("share-other")+"@example.com")
	root := seedRoot(t, owner.ID)
	share := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  root.ID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	repo := shareRepo.New(testPostgres(t))
	if err := repo.Store(context.Background(), &share); err != nil {
		t.Fatalf("Store() error = %v, want nil", err)
	}

	expires := time.Now().UTC().Add(time.Hour)
	if err := repo.UpdateExpiry(context.Background(), other.ID, share.ID, &expires); !errors.Is(err, entity.ErrShareNotFound) {
		t.Errorf("UpdateExpiry(wrong owner) error = %v, want ErrShareNotFound", err)
	}

	if err := repo.Delete(context.Background(), other.ID, share.ID); !errors.Is(err, entity.ErrShareNotFound) {
		t.Errorf("Delete(wrong owner) error = %v, want ErrShareNotFound", err)
	}

	if err := repo.Delete(context.Background(), owner.ID, share.ID); err != nil {
		t.Errorf("Delete(owner) error = %v, want nil", err)
	}
}

func TestShareListIsNewestFirst(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("share-list"), uniqueName("share-list")+"@example.com")
	root := seedRoot(t, owner.ID)
	olderFolder := seedFolder(t, owner.ID, root.ID, "Older")
	newerFolder := seedFolder(t, owner.ID, root.ID, "Newer")

	now := time.Now().UTC()
	older := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  olderFolder.ID,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	newer := entity.Share{
		ID:        uuid.NewString(),
		UserID:    owner.ID,
		FolderID:  newerFolder.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	repo := shareRepo.New(testPostgres(t))
	if err := repo.Store(context.Background(), &older); err != nil {
		t.Fatalf("Store(older) error = %v, want nil", err)
	}

	if err := repo.Store(context.Background(), &newer); err != nil {
		t.Fatalf("Store(newer) error = %v, want nil", err)
	}

	list, err := repo.ListByUser(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("ListByUser() error = %v, want nil", err)
	}

	if len(list) != 2 || list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Errorf("ListByUser() = %+v, want newest first", list)
	}
}
