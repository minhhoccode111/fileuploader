package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

const (
	// _uploadTestFolder is the folder every upload test starts in.
	_uploadTestFolder = "550e8400-e29b-41d4-a716-446655440000"

	// _uploadTestID is the upload session the fake use case hands back.
	_uploadTestID = "7d444840-9dc0-41d1-b245-5ffdce74fad2"

	// _uploadTestPartBytes is the part size the controller is configured with,
	// small enough to cross with one short body.
	_uploadTestPartBytes = int64(8)
)

// fakeUploadFile is a file use case that records the calls of the upload
// handlers and answers with the canned session, file or error of the test.
type fakeUploadFile struct {
	usecase.File

	session  entity.UploadSession
	startErr error

	startedFolder      string
	startedName        string
	startedContentType string
	startedSize        int64

	partNumber int
	partSize   int64
	partBody   string
	partCalls  int
	partErr    error

	file          entity.File
	completeID    string
	completeCalls int
	completeErr   error

	abortedID  string
	abortCalls int
	abortErr   error
}

func (f *fakeUploadFile) StartUpload(
	_ context.Context,
	_, folderID, name, contentType string,
	size int64,
) (entity.UploadSession, error) {
	f.startedFolder = folderID
	f.startedName = name
	f.startedContentType = contentType
	f.startedSize = size

	return f.session, f.startErr
}

func (f *fakeUploadFile) UploadPart(
	_ context.Context,
	_, _ string,
	partNumber int,
	body io.Reader,
	size int64,
) error {
	f.partCalls++
	f.partNumber = partNumber
	f.partSize = size

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	f.partBody = string(data)

	return f.partErr
}

func (f *fakeUploadFile) CompleteUpload(_ context.Context, _, uploadID string) (entity.File, error) {
	f.completeCalls++
	f.completeID = uploadID

	return f.file, f.completeErr
}

func (f *fakeUploadFile) AbortUpload(_ context.Context, _, uploadID string) error {
	f.abortCalls++
	f.abortedID = uploadID

	return f.abortErr
}

// fakeUploadFolder answers the folder listing the entries fragment needs.
type fakeUploadFolder struct {
	usecase.Folder

	view entity.FolderView
	err  error
}

func (f *fakeUploadFolder) List(_ context.Context, _, _ string) (entity.FolderView, error) {
	return f.view, f.err
}

// uploadApp wires the four upload handlers behind a middleware that parks the
// session the auth guard would park.
func uploadApp(files usecase.File, folders usecase.Folder) *fiber.App {
	c := &Controller{
		files:   files,
		folders: folders,
		l:       logger.New("error"),
		v:       validatorx.New(),
		cfg:     &config.Config{},
	}
	c.cfg.Upload.PartBytes = _uploadTestPartBytes

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: "user"})

		return ctx.Next()
	})

	app.Post("/htmx/uploads", c.uploadStart)
	app.Put("/htmx/uploads/:upload/parts/:part", c.uploadPart)
	app.Post("/htmx/uploads/:upload/complete", c.uploadComplete)
	app.Post("/htmx/uploads/:upload/abort", c.uploadAbort)

	return app
}

func TestUploadStartReturnsPartPlan(t *testing.T) {
	t.Parallel()

	files := &fakeUploadFile{
		session: entity.UploadSession{
			ID:        _uploadTestID,
			PartSize:  50 << 20,
			PartCount: 2,
		},
	}

	app := uploadApp(files, &fakeUploadFolder{})

	body := `{"folder":"` + _uploadTestFolder + `","name":" note.txt ",` +
		`"size":104857600,"content_type":"text/plain"}`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/htmx/uploads", strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	var got response.UploadStart
	if decodeErr := json.NewDecoder(res.Body).Decode(&got); decodeErr != nil {
		t.Fatalf("Decode() error = %v, want nil", decodeErr)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	want := response.UploadStart{UploadID: _uploadTestID, PartSize: 50 << 20, PartCount: 2}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}

	if files.startedFolder != _uploadTestFolder {
		t.Errorf("StartUpload folder = %q, want %q", files.startedFolder, _uploadTestFolder)
	}

	if files.startedName != "note.txt" {
		t.Errorf("StartUpload name = %q, want %q", files.startedName, "note.txt")
	}

	if files.startedContentType != "text/plain" {
		t.Errorf("StartUpload content type = %q, want %q", files.startedContentType, "text/plain")
	}

	if files.startedSize != 104857600 {
		t.Errorf("StartUpload size = %d, want %d", files.startedSize, 104857600)
	}
}

func TestUploadStartRejectsAnInvalidBody(t *testing.T) {
	t.Parallel()

	files := &fakeUploadFile{}
	app := uploadApp(files, &fakeUploadFolder{})

	body := `{"folder":"` + _uploadTestFolder + `","size":5,"content_type":"text/plain"}`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/htmx/uploads", strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusUnprocessableEntity)
	}

	if len(raw) == 0 {
		t.Error("body is empty, want the validation message")
	}

	if strings.Contains(string(raw), "<") {
		t.Errorf("body = %q, want plain text", raw)
	}

	if files.startedName != "" {
		t.Errorf("StartUpload ran for an invalid body, name = %q", files.startedName)
	}
}

// A part request must carry a body and a length. A request without one, or
// with more bytes than one part may carry, is refused before the use case is
// asked to store anything.
func TestUploadPartRejectsAWrongContentLength(t *testing.T) {
	t.Parallel()

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()

		files := &fakeUploadFile{}
		app := uploadApp(files, &fakeUploadFolder{})

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPut,
			"/htmx/uploads/"+_uploadTestID+"/parts/1",
			http.NoBody,
		)

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test() error = %v, want nil", err)
		}

		defer res.Body.Close()

		if res.StatusCode != http.StatusLengthRequired {
			t.Errorf("status = %d, want %d", res.StatusCode, http.StatusLengthRequired)
		}

		if files.partCalls != 0 {
			t.Errorf("UploadPart calls = %d, want 0", files.partCalls)
		}
	})

	t.Run("oversized part", func(t *testing.T) {
		t.Parallel()

		files := &fakeUploadFile{}
		app := uploadApp(files, &fakeUploadFolder{})

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPut,
			"/htmx/uploads/"+_uploadTestID+"/parts/1",
			strings.NewReader(strings.Repeat("x", int(_uploadTestPartBytes)+1)),
		)

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test() error = %v, want nil", err)
		}

		defer res.Body.Close()

		if res.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
		}

		if files.partCalls != 0 {
			t.Errorf("UploadPart calls = %d, want 0", files.partCalls)
		}
	})
}

func TestUploadPartPassesTheExactSize(t *testing.T) {
	t.Parallel()

	files := &fakeUploadFile{}
	app := uploadApp(files, &fakeUploadFolder{})

	payload := strings.Repeat("x", int(_uploadTestPartBytes))

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/htmx/uploads/"+_uploadTestID+"/parts/2",
		strings.NewReader(payload),
	)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
	}

	if files.partCalls != 1 {
		t.Fatalf("UploadPart calls = %d, want 1", files.partCalls)
	}

	if files.partNumber != 2 {
		t.Errorf("UploadPart part number = %d, want 2", files.partNumber)
	}

	if files.partSize != _uploadTestPartBytes {
		t.Errorf("UploadPart size = %d, want %d", files.partSize, _uploadTestPartBytes)
	}

	if files.partBody != payload {
		t.Errorf("UploadPart body = %q, want %q", files.partBody, payload)
	}
}

func TestUploadCompleteSetsTheFolderHeader(t *testing.T) {
	t.Parallel()

	const fileID = "9f5d0a3e-4c22-4bb3-9a49-41b2c3d4e5f6"

	files := &fakeUploadFile{
		file: entity.File{
			ID:          fileID,
			Name:        "note.txt",
			FolderID:    _uploadTestFolder,
			ContentType: "text/plain",
			Size:        104857600,
		},
	}
	folders := &fakeUploadFolder{
		view: entity.FolderView{
			Folder: entity.Folder{ID: _uploadTestFolder, Name: "documents"},
			Files: []entity.FileItem{{
				ID:          fileID,
				Name:        "note.txt",
				ContentType: "text/plain",
				Size:        104857600,
			}},
		},
	}

	app := uploadApp(files, folders)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/htmx/uploads/"+_uploadTestID+"/complete",
		http.NoBody,
	)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if got := res.Header.Get("X-Upload-Folder"); got != _uploadTestFolder {
		t.Errorf("X-Upload-Folder = %q, want %q", got, _uploadTestFolder)
	}

	if got := res.Header.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}

	if files.completeCalls != 1 || files.completeID != _uploadTestID {
		t.Errorf("CompleteUpload calls = %d, id = %q, want 1, %q", files.completeCalls, files.completeID, _uploadTestID)
	}

	if !strings.Contains(string(body), "note.txt") {
		t.Errorf("body = %q, want the entries fragment", body)
	}
}

func TestUploadAbortReturnsNoContent(t *testing.T) {
	t.Parallel()

	files := &fakeUploadFile{}
	app := uploadApp(files, &fakeUploadFolder{})

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/htmx/uploads/"+_uploadTestID+"/abort",
		http.NoBody,
	)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
	}

	if files.abortCalls != 1 || files.abortedID != _uploadTestID {
		t.Errorf("AbortUpload calls = %d, id = %q, want 1, %q", files.abortCalls, files.abortedID, _uploadTestID)
	}
}

func TestUploadErrorMapsQuotaAndMissingUpload(t *testing.T) {
	t.Parallel()

	t.Run("quota exceeded", func(t *testing.T) {
		t.Parallel()

		files := &fakeUploadFile{startErr: entity.ErrStorageQuotaExceeded}
		app := uploadApp(files, &fakeUploadFolder{})

		body := `{"folder":"` + _uploadTestFolder + `","name":"note.txt",` +
			`"size":104857600,"content_type":"text/plain"}`

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/htmx/uploads", strings.NewReader(body))
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test() error = %v, want nil", err)
		}

		defer res.Body.Close()

		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("io.ReadAll() error = %v, want nil", err)
		}

		if res.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
		}

		if len(raw) == 0 {
			t.Error("body is empty, want the quota message")
		}

		if strings.Contains(string(raw), "<") {
			t.Errorf("body = %q, want plain text", raw)
		}
	})

	t.Run("missing upload", func(t *testing.T) {
		t.Parallel()

		files := &fakeUploadFile{completeErr: entity.ErrUploadNotFound}
		app := uploadApp(files, &fakeUploadFolder{})

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/htmx/uploads/"+_uploadTestID+"/complete",
			http.NoBody,
		)

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test() error = %v, want nil", err)
		}

		defer res.Body.Close()

		if res.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want %d", res.StatusCode, http.StatusNotFound)
		}
	})
}
