package request

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The edit form carries the canonical limit fields. Stale alias fields must
// not overwrite a value the admin just changed, and clearing a field to zero
// must clear the limit.
//
//nolint:funlen,gocognit // table-driven form binding test
func TestUserUpdateKeepsEditedLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		canonical string
		alias     string
		wantQuota int64
		wantUp    int64
		wantDown  int64
	}{
		{
			name:      "changing an existing limit",
			canonical: "2000",
			alias:     "1000",
			wantQuota: 2000,
			wantUp:    2000,
			wantDown:  2000,
		},
		{
			name:      "clearing an existing limit",
			canonical: "0",
			alias:     "1000",
			wantQuota: 0,
			wantUp:    0,
			wantDown:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got UserUpdate

			app := fiber.New()
			app.Post("/", func(ctx *fiber.Ctx) error {
				if err := ctx.BodyParser(&got); err != nil {
					return err
				}

				got.Sanitize()

				return ctx.SendStatus(http.StatusNoContent)
			})

			form := url.Values{
				"id":                          {"550e8400-e29b-41d4-a716-446655440000"},
				"username":                    {"johndoe"},
				"email":                       {"john@example.com"},
				"role":                        {"user"},
				"storage_quota_bytes":         {tc.canonical},
				"storage_quota":               {tc.alias},
				"quota":                       {tc.alias},
				"upload_rate_bytes_per_sec":   {tc.canonical},
				"upload_rate":                 {tc.alias},
				"upload":                      {tc.alias},
				"download_rate_bytes_per_sec": {tc.canonical},
				"download_rate":               {tc.alias},
				"download":                    {tc.alias},
			}

			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				"/",
				strings.NewReader(form.Encode()),
			)
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)

			res, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test() error = %v, want nil", err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
			}

			if got.StorageQuotaBytes != tc.wantQuota {
				t.Errorf("quota = %d, want %d", got.StorageQuotaBytes, tc.wantQuota)
			}

			if got.UploadRateBytesPerSec != tc.wantUp {
				t.Errorf("upload rate = %d, want %d", got.UploadRateBytesPerSec, tc.wantUp)
			}

			if got.DownloadRateBytesPerSec != tc.wantDown {
				t.Errorf("download rate = %d, want %d", got.DownloadRateBytesPerSec, tc.wantDown)
			}
		})
	}
}
