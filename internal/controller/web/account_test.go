package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

// fakeAccountUser answers the account handlers from canned values and records
// what the forms sent them.
type fakeAccountUser struct {
	usecase.User

	user        entity.User
	getErr      error
	profileErr  error
	passwordErr error

	profileCalled   bool
	profileUserID   string
	profileUsername string
	profileEmail    string

	passwordCalled    bool
	passwordUserID    string
	passwordCurrent   string
	passwordNew       string
	passwordSessionID string
}

func (f *fakeAccountUser) GetUser(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.getErr
}

func (f *fakeAccountUser) UpdateProfile(
	_ context.Context,
	userID, username, email string,
) (entity.User, error) {
	f.profileCalled = true
	f.profileUserID = userID
	f.profileUsername = username
	f.profileEmail = email

	if f.profileErr != nil {
		return entity.User{}, f.profileErr
	}

	return f.user, nil
}

func (f *fakeAccountUser) ChangePassword(
	_ context.Context,
	userID, currentPassword, newPassword, currentSessionID string,
) error {
	f.passwordCalled = true
	f.passwordUserID = userID
	f.passwordCurrent = currentPassword
	f.passwordNew = newPassword
	f.passwordSessionID = currentSessionID

	return f.passwordErr
}

// accountApp wires the account page and its two forms behind a middleware that
// parks the session the auth guard would park.
func accountApp(users usecase.User) *fiber.App {
	c := &Controller{
		u:   users,
		l:   logger.New("error"),
		v:   validatorx.New(),
		cfg: &config.Config{},
	}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: "user-1"})

		return ctx.Next()
	})

	app.Get("/account", c.accountPage)
	app.Post("/htmx/account/profile", c.accountProfileSubmit)
	app.Post("/htmx/account/password", c.accountPasswordSubmit)

	return app
}

// accountForm sends a form to the app and returns the status and the body of
// the answer.
func accountForm(
	t *testing.T,
	app *fiber.App,
	path string,
	form url.Values,
	cookie string,
) (status int, body string) {
	t.Helper()

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		path,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)

	if cookie != "" {
		req.Header.Set(fiber.HeaderCookie, cookie)
	}

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, string(raw)
}

func TestAccountPageRendersTheSignedInAccount(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{user: entity.User{
		ID:        "user-1",
		Username:  "johndoe",
		Email:     "john@example.com",
		CreatedAt: time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC),
	}}

	status, _, body := adminResponse(t, accountApp(users), "/account", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	for _, part := range []string{
		"Manage the name, the email and the password of your account.",
		`value="johndoe"`,
		`value="john@example.com"`,
	} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

func TestAccountProfileSubmitSavesTheSanitizedFields(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{user: entity.User{ID: "user-1", Username: "johndoe", Email: "john@example.com"}}

	status, body := accountForm(t, accountApp(users), "/htmx/account/profile", url.Values{
		"username": {"  johndoe  "},
		"email":    {"  John@Example.COM  "},
	}, "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !users.profileCalled {
		t.Fatal("UpdateProfile() was not called")
	}

	if users.profileUserID != "user-1" {
		t.Errorf("updated account = %q, want %q", users.profileUserID, "user-1")
	}

	if users.profileUsername != "johndoe" || users.profileEmail != "john@example.com" {
		t.Errorf(
			"saved account = %q <%q>, want johndoe <john@example.com>",
			users.profileUsername,
			users.profileEmail,
		)
	}

	if !strings.Contains(body, "Your profile was saved.") {
		t.Errorf("body = %q, want the saved note", body)
	}
}

func TestAccountProfileSubmitReportsATakenEmail(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{profileErr: entity.ErrEmailAlreadyExists}

	status, body := accountForm(t, accountApp(users), "/htmx/account/profile", url.Values{
		"username": {"johndoe"},
		"email":    {"john@example.com"},
	}, "")

	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}

	if !strings.Contains(body, "That email is already registered.") {
		t.Errorf("body = %q, want the taken-email message", body)
	}
}

func TestAccountPasswordSubmitKeepsTheCurrentSession(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{}

	status, body := accountForm(t, accountApp(users), "/htmx/account/password", url.Values{
		"current_password": {"Oldpass1!"},
		"new_password":     {"Newpass1!"},
		"confirm_password": {"Newpass1!"},
	}, "access_token=keep-me")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !users.passwordCalled {
		t.Fatal("ChangePassword() was not called")
	}

	if users.passwordUserID != "user-1" || users.passwordCurrent != "Oldpass1!" ||
		users.passwordNew != "Newpass1!" {
		t.Errorf(
			"changed password of %q from %q to %q, want user-1 from Oldpass1! to Newpass1!",
			users.passwordUserID,
			users.passwordCurrent,
			users.passwordNew,
		)
	}

	if users.passwordSessionID != "keep-me" {
		t.Errorf("kept session = %q, want %q", users.passwordSessionID, "keep-me")
	}

	if !strings.Contains(body, "Your password was changed.") {
		t.Errorf("body = %q, want the saved note", body)
	}
}

func TestAccountPasswordSubmitRejectsAMismatch(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{}

	status, body := accountForm(t, accountApp(users), "/htmx/account/password", url.Values{
		"current_password": {"Oldpass1!"},
		"new_password":     {"Newpass1!"},
		"confirm_password": {"Newpass2!"},
	}, "access_token=keep-me")

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if users.passwordCalled {
		t.Error("ChangePassword() was called with mismatched passwords")
	}

	if !strings.Contains(body, "The new passwords do not match.") {
		t.Errorf("body = %q, want the mismatch message", body)
	}
}

func TestAccountPasswordSubmitReportsAWrongCurrentPassword(t *testing.T) {
	t.Parallel()

	users := &fakeAccountUser{passwordErr: entity.ErrInvalidCredentials}

	status, body := accountForm(t, accountApp(users), "/htmx/account/password", url.Values{
		"current_password": {"Wrongpass1!"},
		"new_password":     {"Newpass1!"},
		"confirm_password": {"Newpass1!"},
	}, "access_token=keep-me")

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if !strings.Contains(body, "Your current password is incorrect.") {
		t.Errorf("body = %q, want the wrong-password message", body)
	}
}
