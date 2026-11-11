package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// uploadBody builds the multipart body the upload drawer sends: the folder
// first, the file second, the name last.
func uploadBody(t *testing.T, folderID, name, filename, content string) (body *bytes.Buffer, contentType string) {
	t.Helper()

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("folder", folderID); err != nil {
		t.Fatalf("WriteField(folder) error = %v, want nil", err)
	}

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v, want nil", err)
	}

	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("part.Write() error = %v, want nil", err)
	}

	if err := writer.WriteField("name", name); err != nil {
		t.Fatalf("WriteField(name) error = %v, want nil", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v, want nil", err)
	}

	return &buf, writer.FormDataContentType()
}

func uploadApp(t *testing.T, maxBytes int64) *fiber.App {
	t.Helper()

	app := fiber.New(fiber.Config{
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
	})

	app.Post("/", func(ctx *fiber.Ctx) error {
		form, err := parseUpload(ctx, maxBytes, 0)
		if err != nil {
			switch {
			case errors.Is(err, entity.ErrFileTooLarge):
				return ctx.Status(http.StatusRequestEntityTooLarge).SendString(err.Error())
			case errors.Is(err, entity.ErrInvalidUpload):
				return ctx.Status(http.StatusUnprocessableEntity).SendString(err.Error())
			default:
				return ctx.Status(http.StatusInternalServerError).SendString(err.Error())
			}
		}
		defer form.cleanup()

		return ctx.SendString(fmt.Sprintf(
			"%s|%s|%d|%s",
			form.folderID,
			form.name,
			form.size,
			form.contentType,
		))
	})

	return app
}

func TestParseUploadReadsFieldsAndFile(t *testing.T) {
	t.Parallel()

	body, contentType := uploadBody(
		t,
		"550e8400-e29b-41d4-a716-446655440000",
		"greeting.txt",
		"ignored-name.txt",
		"hello",
	)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
	req.Header.Set(fiber.HeaderContentType, contentType)

	res, err := uploadApp(t, 1<<20).Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw := new(bytes.Buffer)
	if _, err := raw.ReadFrom(res.Body); err != nil {
		t.Fatalf("ReadFrom() error = %v, want nil", err)
	}

	want := "550e8400-e29b-41d4-a716-446655440000|greeting.txt|5|application/octet-stream"
	if res.StatusCode != http.StatusOK || raw.String() != want {
		t.Fatalf("status = %d, body = %q, want 200 %q", res.StatusCode, raw.String(), want)
	}
}

func TestParseUploadRejectsAnOversizedFile(t *testing.T) {
	t.Parallel()

	body, contentType := uploadBody(t, "550e8400-e29b-41d4-a716-446655440000", "greeting.txt", "greeting.txt", "hello")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
	req.Header.Set(fiber.HeaderContentType, contentType)

	res, err := uploadApp(t, 4).Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

func TestUploadBodyLimitRejectsOverLimit(t *testing.T) {
	t.Parallel()

	body := &_uploadBodyLimit{reader: strings.NewReader("hello"), left: 4}

	if _, err := io.ReadAll(body); !errors.Is(err, entity.ErrFileTooLarge) {
		t.Fatalf("ReadAll() error = %v, want %v", err, entity.ErrFileTooLarge)
	}
}

func TestParseUploadRejectsAMissingFile(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("folder", "550e8400-e29b-41d4-a716-446655440000"); err != nil {
		t.Fatalf("WriteField(folder) error = %v, want nil", err)
	}

	if err := writer.WriteField("name", "greeting.txt"); err != nil {
		t.Fatalf("WriteField(name) error = %v, want nil", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v, want nil", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", &buf)
	req.Header.Set(fiber.HeaderContentType, writer.FormDataContentType())

	res, err := uploadApp(t, 1<<20).Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusUnprocessableEntity)
	}
}
