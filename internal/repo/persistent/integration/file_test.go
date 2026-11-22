//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	fileRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/file"
)

func TestFileRepoScopesReadsToTheOwner(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("owner"), uniqueName("owner")+"@example.com")
	other := seedUser(t, uniqueName("other"), uniqueName("other")+"@example.com")
	root := seedRoot(t, owner.ID)
	otherRoot := seedRoot(t, other.ID)
	file := seedFile(t, owner.ID, root.ID, "note.txt", uniqueName("key"), 5)

	repo := fileRepo.New(testPostgres(t))
	ctx := context.Background()

	if _, err := repo.Get(ctx, other.ID, file.ID); !errors.Is(err, entity.ErrFileNotFound) {
		t.Errorf("Get(wrong owner) error = %v, want ErrFileNotFound", err)
	}

	list, err := repo.ListPage(ctx, other.ID, otherRoot.ID, "", nil, 10)
	if err != nil {
		t.Fatalf("ListPage() error = %v, want nil", err)
	}

	if len(list) != 0 {
		t.Errorf("ListPage(wrong owner) = %+v, want no rows", list)
	}
}

func TestFilePageIsStableAcrossTies(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("tie"), uniqueName("tie")+"@example.com")
	root := seedRoot(t, owner.ID)
	firstFolder := seedFolder(t, owner.ID, root.ID, "First")
	secondFolder := seedFolder(t, owner.ID, root.ID, "Second")
	seedFile(t, owner.ID, firstFolder.ID, "same.txt", uniqueName("key"), 1)
	seedFile(t, owner.ID, secondFolder.ID, "same.txt", uniqueName("key"), 1)

	repo := fileRepo.New(testPostgres(t))
	ctx := context.Background()

	var (
		seen  = map[string]bool{}
		after *entity.EntryCursor
		total int
	)

	for range 10 {
		page, err := repo.ListPage(ctx, owner.ID, "", "", after, 1)
		if err != nil {
			t.Fatalf("ListPage() error = %v, want nil", err)
		}

		if len(page) == 0 {
			break
		}

		for i := range page {
			if seen[page[i].ID] {
				t.Fatalf("file %q appeared twice", page[i].ID)
			}

			seen[page[i].ID] = true
			total++
		}

		last := page[len(page)-1]
		after = &entity.EntryCursor{Kind: entity.EntryKindFile, Name: last.Name, ID: last.ID}
	}

	if total != 2 {
		t.Errorf("paged files = %d, want 2 without duplicates or omissions", total)
	}
}

func TestFileSearchEscapesWildcards(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("search"), uniqueName("search")+"@example.com")
	root := seedRoot(t, owner.ID)
	seedFile(t, owner.ID, root.ID, "100%_x", uniqueName("key"), 1)
	seedFile(t, owner.ID, root.ID, "100ax", uniqueName("key"), 1)
	seedFile(t, owner.ID, root.ID, "100_x", uniqueName("key"), 1)

	page, err := fileRepo.New(testPostgres(t)).
		ListPage(context.Background(), owner.ID, root.ID, "100%", nil, 10)
	if err != nil {
		t.Fatalf("ListPage() error = %v, want nil", err)
	}

	if len(page) != 1 || page[0].Name != "100%_x" {
		t.Errorf("ListPage() = %+v, want only the literal 100%%_x", page)
	}
}

func TestFileRepoEnforcesUniqueNamePerFolder(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("unique"), uniqueName("unique")+"@example.com")
	root := seedRoot(t, owner.ID)
	seedFile(t, owner.ID, root.ID, "Report.txt", uniqueName("key"), 1)

	repo := fileRepo.New(testPostgres(t))
	duplicate := entity.File{
		ID:          "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b",
		UserID:      owner.ID,
		FolderID:    root.ID,
		Name:        "report.txt",
		ContentType: "text/plain",
		StorageKey:  uniqueName("key"),
		Size:        1,
		CreatedAt:   nowUTC(),
		UpdatedAt:   nowUTC(),
	}

	if err := repo.Store(context.Background(), &duplicate); !errors.Is(err, entity.ErrEntryAlreadyExists) {
		t.Errorf("Store(duplicate) error = %v, want ErrEntryAlreadyExists", err)
	}
}

func TestDeleteByFolderSubtreeReturnsEveryFile(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("subtree"), uniqueName("subtree")+"@example.com")
	root := seedRoot(t, owner.ID)
	child := seedFolder(t, owner.ID, root.ID, "Child")
	seedFile(t, owner.ID, child.ID, "note.txt", uniqueName("key"), 1)

	repo := fileRepo.New(testPostgres(t))
	files, err := repo.DeleteByFolderSubtree(context.Background(), owner.ID, root.ID)
	if err != nil {
		t.Fatalf("DeleteByFolderSubtree() error = %v, want nil", err)
	}

	if len(files) != 1 || files[0].Name != "note.txt" {
		t.Errorf("DeleteByFolderSubtree() = %+v, want note.txt", files)
	}

	if _, err := repo.Get(context.Background(), owner.ID, files[0].ID); !errors.Is(err, entity.ErrFileNotFound) {
		t.Errorf("Get(deleted file) error = %v, want ErrFileNotFound", err)
	}
}
