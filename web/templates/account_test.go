package templates

import (
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
)

// The account page carries the two forms its holder owns: the profile form
// with the values the account holds now, and the password form.
func TestAccountContentCarriesTheProfileAndPasswordForms(t *testing.T) {
	t.Parallel()

	account := response.UserRow{
		Username: "johndoe",
		Email:    "john@example.com",
		Joined:   "2026-01-02 15:04",
	}

	html := renderTemplate(t, AccountContent(account))

	want := []string{
		`value="johndoe"`,
		`value="john@example.com"`,
		"2026-01-02 15:04",
		`id="account-profile-panel"`,
		`hx-post="/htmx/account/profile"`,
		`hx-target="#account-profile-panel"`,
		`id="account-profile-error"`,
		`id="account-password-panel"`,
		`hx-post="/htmx/account/password"`,
		`hx-target="#account-password-panel"`,
		`id="account-password-error"`,
		`name="current_password"`,
		`name="new_password"`,
		`name="confirm_password"`,
		`autocomplete="current-password"`,
		`autocomplete="new-password"`,
	}

	for _, part := range want {
		if !strings.Contains(html, part) {
			t.Errorf("AccountContent() = %s, want it to contain %q", html, part)
		}
	}
}

// A panel says what the save did above the form, so the visitor knows the
// request landed without leaving the page.
func TestAccountPanelsShowTheSaveNote(t *testing.T) {
	t.Parallel()

	account := response.UserRow{Username: "johndoe", Email: "john@example.com"}

	profile := renderTemplate(t, AccountProfilePanel(account, "Your profile was saved."))
	if !strings.Contains(profile, "alert-success") || !strings.Contains(profile, "Your profile was saved.") {
		t.Errorf("AccountProfilePanel() = %s, want the saved note", profile)
	}

	password := renderTemplate(t, AccountPasswordPanel("Your password was changed."))
	if !strings.Contains(password, "alert-success") || !strings.Contains(password, "Your password was changed.") {
		t.Errorf("AccountPasswordPanel() = %s, want the saved note", password)
	}
}
