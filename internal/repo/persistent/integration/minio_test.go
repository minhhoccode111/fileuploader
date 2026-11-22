//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

func TestObjectStorageRoundTrip(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("object")
	body := []byte("hello from object storage")

	if err := storage.PutObject(context.Background(), bucket, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("PutObject() error = %v, want nil", err)
	}

	reader, err := storage.GetObject(context.Background(), bucket, key)
	if err != nil {
		t.Fatalf("GetObject() error = %v, want nil", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestPresignedGetReturnsTheObject(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("presigned")
	body := []byte("presigned body")

	if err := storage.PutObject(context.Background(), bucket, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("PutObject() error = %v, want nil", err)
	}

	link, err := storage.PresignedGetObject(context.Background(), bucket, key, time.Minute, "note.txt")
	if err != nil {
		t.Fatalf("PresignedGetObject() error = %v, want nil", err)
	}

	res, err := http.Get(link) //nolint:noctx // the signed URL is the test subject.
	if err != nil {
		t.Fatalf("http.Get() error = %v, want nil", err)
	}

	defer res.Body.Close()

	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Errorf("GET = %d %q, want 200 %q", res.StatusCode, got, body)
	}
}

func TestCopyAndRemoveObject(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	source := uniqueName("source")
	copied := uniqueName("copied")
	body := []byte("copy me")

	if err := storage.PutObject(context.Background(), bucket, source, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("PutObject() error = %v, want nil", err)
	}

	if err := storage.CopyObject(context.Background(), bucket, copied, source); err != nil {
		t.Fatalf("CopyObject() error = %v, want nil", err)
	}

	for _, key := range []string{source, copied} {
		reader, err := storage.GetObject(context.Background(), bucket, key)
		if err != nil {
			t.Fatalf("GetObject(%q) error = %v, want nil", key, err)
		}

		got, readErr := io.ReadAll(reader)
		reader.Close()

		if readErr != nil || !bytes.Equal(got, body) {
			t.Errorf("GetObject(%q) = %q, %v; want %q", key, got, readErr, body)
		}
	}

	if err := storage.RemoveObject(context.Background(), bucket, copied); err != nil {
		t.Fatalf("RemoveObject() error = %v, want nil", err)
	}

	reader, err := storage.GetObject(context.Background(), bucket, copied)
	if err != nil {
		return
	}
	defer reader.Close()

	if _, err := io.ReadAll(reader); err == nil {
		t.Error("ReadAll(removed copy) error = nil, want a missing object")
	}
}

func TestObjectStorageRejectsAMissingObject(t *testing.T) {
	t.Parallel()

	reader, err := testStorage(t).GetObject(context.Background(), testBucket(), uniqueName("missing"))
	if err != nil {
		return
	}
	defer reader.Close()

	if data, readErr := io.ReadAll(reader); readErr == nil && len(data) > 0 {
		t.Errorf("ReadAll(missing) = %q, %v; want an error or no bytes", data, readErr)
	}
}

func TestMultipartUploadCompletes(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("multipart")
	ctx := context.Background()

	uploadID, err := storage.CreateMultipartUpload(ctx, bucket, key, "application/octet-stream")
	if err != nil {
		t.Fatalf("CreateMultipartUpload() error = %v, want nil", err)
	}

	fiveMiB := bytes.Repeat([]byte("a"), 5<<20)
	final := []byte("done")
	parts := make([]multipart.Part, 0, 3)

	for number, partBody := range [][]byte{fiveMiB, fiveMiB, final} {
		etag, uploadErr := storage.UploadPart(
			ctx,
			bucket,
			key,
			uploadID,
			number+1,
			bytes.NewReader(partBody),
			int64(len(partBody)),
		)
		if uploadErr != nil {
			t.Fatalf("UploadPart(%d) error = %v, want nil", number+1, uploadErr)
		}

		parts = append(parts, multipart.Part{Number: number + 1, ETag: etag})
	}

	if err := storage.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		t.Fatalf("CompleteMultipartUpload() error = %v, want nil", err)
	}

	reader, err := storage.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatalf("GetObject() error = %v, want nil", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if len(got) != 2*(5<<20)+len(final) || !bytes.HasSuffix(got, final) {
		t.Errorf("object length = %d, want %d with the final part", len(got), 2*(5<<20)+len(final))
	}
}

func TestMultipartAbortLeavesNoObject(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("abort")
	ctx := context.Background()

	uploadID, err := storage.CreateMultipartUpload(ctx, bucket, key, "application/octet-stream")
	if err != nil {
		t.Fatalf("CreateMultipartUpload() error = %v, want nil", err)
	}

	if _, err := storage.UploadPart(ctx, bucket, key, uploadID, 1, bytes.NewReader(bytes.Repeat([]byte("a"), 5<<20)), 5<<20); err != nil {
		t.Fatalf("UploadPart() error = %v, want nil", err)
	}

	if err := storage.AbortMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		t.Fatalf("AbortMultipartUpload() error = %v, want nil", err)
	}

	reader, err := storage.GetObject(ctx, bucket, key)
	if err != nil {
		return
	}
	defer reader.Close()

	if data, readErr := io.ReadAll(reader); readErr == nil && len(data) > 0 {
		t.Errorf("ReadAll(aborted) = %q, %v; want no object", data, readErr)
	}
}

func TestListIncompleteUploadsReportsAnOpenUpload(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("incomplete")
	ctx := context.Background()

	uploadID, err := storage.CreateMultipartUpload(ctx, bucket, key, "application/octet-stream")
	if err != nil {
		t.Fatalf("CreateMultipartUpload() error = %v, want nil", err)
	}
	defer storage.AbortMultipartUpload(ctx, bucket, key, uploadID)

	uploads, err := storage.ListIncompleteUploads(ctx, bucket)
	if err != nil {
		t.Fatalf("ListIncompleteUploads() error = %v, want nil", err)
	}

	for _, upload := range uploads {
		if upload.Key == key && upload.UploadID == uploadID {
			return
		}
	}

	t.Errorf("ListIncompleteUploads() = %+v, want key %q and upload %q", uploads, key, uploadID)
}

func TestEnsureBucketAcceptsAnExistingBucket(t *testing.T) {
	t.Parallel()

	if err := testStorage(t).EnsureBucket(context.Background(), testBucket()); err != nil {
		t.Fatalf("EnsureBucket() error = %v, want nil", err)
	}
}

func TestRemoveObjectIsIdempotent(t *testing.T) {
	t.Parallel()

	storage := testStorage(t)
	bucket := testBucket()
	key := uniqueName("remove")

	if err := storage.RemoveObject(context.Background(), bucket, key); err != nil {
		t.Fatalf("RemoveObject(first) error = %v, want nil", err)
	}

	if err := storage.RemoveObject(context.Background(), bucket, key); err != nil {
		t.Fatalf("RemoveObject(second) error = %v, want nil", err)
	}
}
