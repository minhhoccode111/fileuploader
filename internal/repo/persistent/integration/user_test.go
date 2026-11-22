//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	userRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/user"
)

func TestUserListFiltersEscapedSearch(t *testing.T) {
	t.Parallel()

	prefix := uniqueName("escaped")
	first := seedUser(t, prefix+"%_one", prefix+"-one@example.com")
	seedUser(t, prefix+"_two", prefix+"-two@example.com")
	seedUser(t, prefix+"xthree", prefix+"-three@example.com")

	filter := entity.UserFilter{Query: prefix + "%_", Limit: 10, Page: 1}
	filter.Sanitize()

	list, err := userRepo.New(testPostgres(t)).List(context.Background(), &filter)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(list.Items) != 1 || list.Items[0].ID != first.ID {
		t.Errorf("List() = %+v, want only the literal %q", list.Items, first.Username)
	}
}

func TestUserListOrdersAndPages(t *testing.T) {
	t.Parallel()

	prefix := uniqueName("ordered")
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f"} {
		seedUser(t, prefix+"-"+suffix, prefix+"-"+suffix+"@example.com")
	}

	repo := userRepo.New(testPostgres(t))
	filter := entity.UserFilter{
		Query: prefix,
		Sort:  entity.UserSortUsername,
		Dir:   entity.UserSortAscending,
		Limit: 5,
		Page:  1,
	}
	filter.Sanitize()

	firstPage, err := repo.List(context.Background(), &filter)
	if err != nil {
		t.Fatalf("List(page 1) error = %v, want nil", err)
	}

	if len(firstPage.Items) != 5 {
		t.Fatalf("page 1 items = %d, want 5", len(firstPage.Items))
	}

	for i, suffix := range []string{"a", "b", "c", "d", "e"} {
		want := prefix + "-" + suffix
		if firstPage.Items[i].Username != want {
			t.Errorf("page 1 item %d = %q, want %q", i, firstPage.Items[i].Username, want)
		}
	}

	filter.Page = 2
	secondPage, err := repo.List(context.Background(), &filter)
	if err != nil {
		t.Fatalf("List(page 2) error = %v, want nil", err)
	}

	if len(secondPage.Items) != 1 || secondPage.Items[0].Username != prefix+"-f" {
		t.Errorf("page 2 = %+v, want only f", secondPage.Items)
	}
}

func TestUserListTotalCountsTheWholeFilter(t *testing.T) {
	t.Parallel()

	prefix := uniqueName("total")
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f"} {
		seedUser(t, prefix+"-"+suffix, prefix+"-"+suffix+"@example.com")
	}

	filter := entity.UserFilter{Query: prefix, Limit: 5, Page: 1}
	filter.Sanitize()

	list, err := userRepo.New(testPostgres(t)).List(context.Background(), &filter)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(list.Items) != 5 {
		t.Errorf("items = %d, want 5", len(list.Items))
	}

	if list.Total != 6 {
		t.Errorf("Total = %d, want 6", list.Total)
	}
}

func TestUserUniqueConstraints(t *testing.T) {
	t.Parallel()

	existing := seedUser(t, uniqueName("unique-user"), uniqueName("unique-email")+"@example.com")
	repo := userRepo.New(testPostgres(t))

	duplicateEmail := entity.User{
		ID:           uuid.NewString(),
		Username:     uniqueName("other"),
		Email:        existing.Email,
		PasswordHash: "hash",
		Role:         entity.UserRoleUser,
		Status:       entity.UserStatusActive,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := repo.Store(context.Background(), &duplicateEmail); !errors.Is(err, entity.ErrEmailAlreadyExists) {
		t.Errorf("Store(duplicate email) error = %v, want ErrEmailAlreadyExists", err)
	}

	duplicateUsername := entity.User{
		ID:           uuid.NewString(),
		Username:     existing.Username,
		Email:        uniqueName("unique-email") + "@example.com",
		PasswordHash: "hash",
		Role:         entity.UserRoleUser,
		Status:       entity.UserStatusActive,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := repo.Store(context.Background(), &duplicateUsername); !errors.Is(err, entity.ErrUsernameAlreadyExists) {
		t.Errorf("Store(duplicate username) error = %v, want ErrUsernameAlreadyExists", err)
	}
}
