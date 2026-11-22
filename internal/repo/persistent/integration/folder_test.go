//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	fileRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/file"
	folderRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/folder"
)

func TestFolderBreadcrumbIsRootFirst(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("crumb"), uniqueName("crumb")+"@example.com")
	root := seedRoot(t, owner.ID)
	child := seedFolder(t, owner.ID, root.ID, "Child")
	leaf := seedFolder(t, owner.ID, child.ID, "Leaf")

	refs, err := folderRepo.New(testPostgres(t)).
		Breadcrumb(context.Background(), owner.ID, leaf.ID)
	if err != nil {
		t.Fatalf("Breadcrumb() error = %v, want nil", err)
	}

	if len(refs) != 3 || refs[0].ID != root.ID || refs[1].ID != child.ID || refs[2].ID != leaf.ID {
		t.Errorf("Breadcrumb() = %+v, want root, child, leaf", refs)
	}
}

func TestFolderPathsResolvesManyFolders(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("path"), uniqueName("path")+"@example.com")
	root := seedRoot(t, owner.ID)
	child := seedFolder(t, owner.ID, root.ID, "Child")
	leaf := seedFolder(t, owner.ID, child.ID, "Leaf")

	paths, err := folderRepo.New(testPostgres(t)).
		Paths(context.Background(), owner.ID, []string{child.ID, leaf.ID})
	if err != nil {
		t.Fatalf("Paths() error = %v, want nil", err)
	}

	if got := paths[child.ID]; len(got) != 2 || got[0].ID != root.ID || got[1].ID != child.ID {
		t.Errorf("child path = %+v, want root then child", got)
	}

	if got := paths[leaf.ID]; len(got) != 3 || got[0].ID != root.ID || got[2].ID != leaf.ID {
		t.Errorf("leaf path = %+v, want root, child, leaf", got)
	}
}

func TestFolderPathsOmitsAnotherOwnersFolder(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("owner"), uniqueName("owner")+"@example.com")
	other := seedUser(t, uniqueName("other"), uniqueName("other")+"@example.com")
	root := seedRoot(t, owner.ID)
	child := seedFolder(t, owner.ID, root.ID, "Child")

	paths, err := folderRepo.New(testPostgres(t)).
		Paths(context.Background(), other.ID, []string{child.ID})
	if err != nil {
		t.Fatalf("Paths() error = %v, want nil", err)
	}

	if _, ok := paths[child.ID]; ok {
		t.Errorf("Paths() leaked %q to another owner", child.ID)
	}
}

func TestFolderPageScopesToParentAndSearch(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("page"), uniqueName("page")+"@example.com")
	other := seedUser(t, uniqueName("other"), uniqueName("other")+"@example.com")
	root := seedRoot(t, owner.ID)
	otherRoot := seedRoot(t, other.ID)
	seedFolder(t, owner.ID, root.ID, "Docs")
	seedFolder(t, owner.ID, root.ID, "Media")
	seedFolder(t, other.ID, otherRoot.ID, "Docs")

	page, err := folderRepo.New(testPostgres(t)).
		ListPage(context.Background(), owner.ID, root.ID, "doc", nil, 10)
	if err != nil {
		t.Fatalf("ListPage() error = %v, want nil", err)
	}

	if len(page) != 1 || page[0].Name != "Docs" {
		t.Errorf("ListPage() = %+v, want only Docs", page)
	}
}

func TestFolderLocksRejectUnknownFolders(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("lock"), uniqueName("lock")+"@example.com")
	repo := folderRepo.New(testPostgres(t))

	if _, err := repo.Lock(context.Background(), owner.ID, "550e8400-e29b-41d4-a716-446655440000"); !errors.Is(err, entity.ErrFolderNotFound) {
		t.Errorf("Lock() error = %v, want ErrFolderNotFound", err)
	}

	if err := repo.LockSubtree(context.Background(), owner.ID, "550e8400-e29b-41d4-a716-446655440000"); !errors.Is(err, entity.ErrFolderNotFound) {
		t.Errorf("LockSubtree() error = %v, want ErrFolderNotFound", err)
	}
}

func TestFolderDeleteCascadesFiles(t *testing.T) {
	t.Parallel()

	owner := seedUser(t, uniqueName("cascade"), uniqueName("cascade")+"@example.com")
	root := seedRoot(t, owner.ID)
	child := seedFolder(t, owner.ID, root.ID, "Child")
	file := seedFile(t, owner.ID, child.ID, "note.txt", uniqueName("key"), 1)

	if err := folderRepo.New(testPostgres(t)).Delete(context.Background(), owner.ID, child.ID); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if _, err := fileRepo.New(testPostgres(t)).Get(context.Background(), owner.ID, file.ID); !errors.Is(err, entity.ErrFileNotFound) {
		t.Errorf("Get(cascaded file) error = %v, want ErrFileNotFound", err)
	}
}
