package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

type fakeDownloadFile struct {
	usecase.File

	reader io.ReadCloser
	file   entity.File
}

func (f *fakeDownloadFile) OpenDownload(
	_ context.Context,
	_, _ string,
) (io.ReadCloser, entity.File, error) {
	return f.reader, f.file, nil
}

type fakeDownloadUser struct {
	usecase.User

	user entity.User
}

func (f *fakeDownloadUser) GetUser(_ context.Context, _ string) (entity.User, error) {
	return f.user, nil
}

// closeAwareReader reports a read after close, which is what a download sees
// when the handler closes the object before the response writes it.
type closeAwareReader struct {
	data   []byte
	closed bool
}

func (r *closeAwareReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	if len(r.data) == 0 {
		return 0, io.EOF
	}

	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, nil
}

func (r *closeAwareReader) Close() error {
	r.closed = true

	return nil
}

// The download handler must leave the object open until the server writes the
// response; the server closes it afterwards.
func TestFileDownloadKeepsTheStreamOpen(t *testing.T) {
	t.Parallel()

	const fileID = "550e8400-e29b-41d4-a716-446655440000"

	c := &Controller{
		u: &fakeDownloadUser{},
		files: &fakeDownloadFile{
			reader: &closeAwareReader{data: []byte("hello")},
			file: entity.File{
				ID:          fileID,
				Name:        "note.txt",
				ContentType: "text/plain",
				Size:        5,
			},
		},
		l:   logger.New("error"),
		v:   validatorx.New(),
		cfg: &config.Config{},
	}

	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: "user"})

		return ctx.Next()
	})
	app.Get("/files/:file/download", c.fileDownload)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/files/"+fileID+"/download",
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
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	if got := res.Header.Get(fiber.HeaderContentLength); got != "5" {
		t.Errorf("Content-Length = %q, want %q", got, "5")
	}

	checkDownloadHeaders(t, res, "note.txt")
}

// checkDownloadHeaders asserts what every attachment must carry: the exact
// name in filename* and the no-sniff guard.
func checkDownloadHeaders(t *testing.T, res *http.Response, name string) {
	t.Helper()

	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}

	disposition := res.Header.Get(fiber.HeaderContentDisposition)
	if !strings.Contains(disposition, "filename*=UTF-8''"+name) {
		t.Errorf("Content-Disposition = %q, want the exact name %q in filename*", disposition, name)
	}
}

// A streamed body without a positive length is sent chunked, so an empty file
// carries no Content-Length rather than a zero one.
func TestStreamLengthRefusesNonPositiveSizes(t *testing.T) {
	t.Parallel()

	for _, size := range []int64{0, -1} {
		if length, ok := streamLength(size); ok {
			t.Errorf("streamLength(%d) = %d, true; want false", size, length)
		}
	}
}

// A size that fits the int Fiber takes comes back unchanged. A size beyond
// that range is refused, so a 32-bit build streams chunked instead of sending
// a negative or truncated Content-Length.
func TestStreamLengthKeepsASizeThatFits(t *testing.T) {
	t.Parallel()

	const size = int64(5) << 30

	length, ok := streamLength(size)
	if strconv.IntSize == 64 {
		if !ok || int64(length) != size {
			t.Errorf("streamLength(%d) = %d, %v; want the size back", size, length, ok)
		}

		return
	}

	if ok {
		t.Errorf("streamLength(%d) = %d, true; want false over the int range", size, length)
	}
}
