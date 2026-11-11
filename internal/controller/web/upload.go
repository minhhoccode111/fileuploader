package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

const (
	// _maxUploadFieldBytes bounds one non-file form field, so a malformed form
	// cannot make the server buffer an unbounded field.
	_maxUploadFieldBytes = 1 << 20

	// _maxUploadParts bounds the number of multipart parts in one upload form.
	_maxUploadParts = 16
)

// _uploadForm is the multipart form of one file upload after it was read
// through the throttled request body. The file is a temporary file the caller
// must clean up.
type _uploadForm struct {
	folderID    string
	name        string
	file        *os.File
	size        int64
	contentType string
}

// cleanup closes and removes the temporary file the upload was read into.
func (f *_uploadForm) cleanup() {
	if f.file == nil {
		return
	}

	name := f.file.Name()

	_ = f.file.Close()
	_ = os.Remove(name)
}

// _uploadBodyLimit stops the raw multipart body at the total upload limit,
// including the envelope around the file.
type _uploadBodyLimit struct {
	reader io.Reader
	left   int64
}

// Read returns the body bytes until the limit, then reports ErrFileTooLarge.
func (l *_uploadBodyLimit) Read(p []byte) (int, error) {
	if l.left <= 0 {
		var probe [1]byte

		n, err := l.reader.Read(probe[:])
		if n > 0 {
			return 0, entity.ErrFileTooLarge
		}

		return 0, err
	}

	if int64(len(p)) > l.left {
		p = p[:l.left]
	}

	n, err := l.reader.Read(p)
	l.left -= int64(n)

	return n, err
}

// parseUpload reads the multipart upload form from the request body through a
// rate limiter, so the client is throttled while it sends the bytes. The file
// is stored in a temporary file because its size must be known before the use
// case writes it to the bucket.
func parseUpload(ctx *fiber.Ctx, maxBytes, rateLimit int64) (_uploadForm, error) {
	boundary, err := uploadBoundary(ctx.Get(fiber.HeaderContentType))
	if err != nil {
		return _uploadForm{}, err
	}

	body := ctx.Request().BodyStream()
	if body == nil {
		body = bytes.NewReader(ctx.Request().Body())
	}

	body = &_uploadBodyLimit{
		reader: body,
		left:   maxBytes + config.MultipartOverheadBytes,
	}

	throttled := newThrottledReader(ctx.UserContext(), body, rateLimit)
	defer throttled.Close()

	form := _uploadForm{}

	reader := multipart.NewReader(throttled, boundary)
	foundEnd := false

	for range _maxUploadParts {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			foundEnd = true

			break
		}

		if partErr != nil {
			form.cleanup()

			return _uploadForm{}, fmt.Errorf("parseUpload - reader.NextPart: %w", partErr)
		}

		readErr := readUploadPart(part, &form, maxBytes)
		closeErr := part.Close()

		if readErr != nil {
			form.cleanup()

			return _uploadForm{}, readErr
		}

		if closeErr != nil {
			form.cleanup()

			return _uploadForm{}, fmt.Errorf("parseUpload - part.Close: %w", closeErr)
		}
	}

	if !foundEnd || form.file == nil {
		form.cleanup()

		return _uploadForm{}, entity.ErrInvalidUpload
	}

	return form, nil
}

// readUploadPart routes one multipart part to the form field it names.
func readUploadPart(part *multipart.Part, form *_uploadForm, maxBytes int64) error {
	switch part.FormName() {
	case "folder":
		value, err := readUploadField(part)
		if err != nil {
			return err
		}

		form.folderID = value
	case "name":
		value, err := readUploadField(part)
		if err != nil {
			return err
		}

		form.name = value
	case "file":
		return readUploadFile(part, form, maxBytes)
	default:
		return discardUploadPart(part)
	}

	return nil
}

// readUploadField reads one small text field of the form.
func readUploadField(part *multipart.Part) (string, error) {
	data, err := io.ReadAll(io.LimitReader(part, _maxUploadFieldBytes+1))
	if err != nil {
		return "", fmt.Errorf("readUploadField - io.ReadAll: %w", err)
	}

	if len(data) > _maxUploadFieldBytes {
		return "", entity.ErrInvalidUpload
	}

	return strings.TrimSpace(string(data)), nil
}

// readUploadFile copies the file part into a temporary file, bounded by the
// server upload limit.
func readUploadFile(part *multipart.Part, form *_uploadForm, maxBytes int64) error {
	if form.file != nil {
		return entity.ErrInvalidUpload
	}

	tmp, err := os.CreateTemp("", "fileuploader-upload-*")
	if err != nil {
		return fmt.Errorf("readUploadFile - os.CreateTemp: %w", err)
	}

	size, copyErr := io.Copy(tmp, io.LimitReader(part, maxBytes+1))
	if copyErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("readUploadFile - io.Copy: %w", copyErr)
	}

	if size > maxBytes {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return entity.ErrFileTooLarge
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("readUploadFile - tmp.Seek: %w", err)
	}

	form.file = tmp
	form.size = size
	form.contentType = part.Header.Get(fiber.HeaderContentType)

	return nil
}

// discardUploadPart drains a part the form does not use, bounded like a field.
func discardUploadPart(part *multipart.Part) error {
	size, err := io.Copy(io.Discard, io.LimitReader(part, _maxUploadFieldBytes+1))
	if err != nil {
		return fmt.Errorf("discardUploadPart - io.Copy: %w", err)
	}

	if size > _maxUploadFieldBytes {
		return entity.ErrInvalidUpload
	}

	return nil
}

// uploadBoundary reads the multipart boundary from the request content type.
func uploadBoundary(contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return "", entity.ErrInvalidUpload
	}

	return params["boundary"], nil
}
