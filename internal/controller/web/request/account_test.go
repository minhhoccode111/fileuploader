package request

import "testing"

func TestProfileUpdateSanitizesTheInput(t *testing.T) {
	t.Parallel()

	req := ProfileUpdate{Username: "  johndoe  ", Email: "  John@Example.COM  "}

	req.Sanitize()

	if req.Username != "johndoe" {
		t.Errorf("username = %q, want %q", req.Username, "johndoe")
	}

	if req.Email != "john@example.com" {
		t.Errorf("email = %q, want %q", req.Email, "john@example.com")
	}
}

func TestPasswordUpdateReportsWhetherTheNewPasswordsMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		req     PasswordUpdate
		matches bool
	}{
		{
			name:    "the same password twice",
			req:     PasswordUpdate{NewPassword: "newpass1!", ConfirmPassword: "newpass1!"},
			matches: true,
		},
		{
			name:    "a different confirmation",
			req:     PasswordUpdate{NewPassword: "newpass1!", ConfirmPassword: "newpass2!"},
			matches: false,
		},
		{
			name:    "a case difference",
			req:     PasswordUpdate{NewPassword: "newpass1!", ConfirmPassword: "NEWPASS1!"},
			matches: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.req.Matches(); got != tc.matches {
				t.Errorf("Matches() = %t, want %t", got, tc.matches)
			}
		})
	}
}
