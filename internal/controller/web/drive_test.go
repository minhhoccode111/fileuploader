package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// A drive action that would push the account past its quota is a client
// problem, not a server fault: the handler must answer 413 and say why.
func TestDriveErrorMapsStorageQuotaExceeded(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	c := &Controller{l: logger.New("error")}

	app.Get("/", func(ctx *fiber.Ctx) error {
		return c.driveError(ctx, entity.ErrStorageQuotaExceeded, "test")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
	}

	if len(body) == 0 {
		t.Error("body is empty, want the quota message")
	}
}
