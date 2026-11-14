package minio

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"slices"
	"time"

	"github.com/minhhoccode111/fileuploader/pkg/multipart"
	miniogo "github.com/minio/minio-go/v7"
)

// PutObject streams one object into the bucket. The size comes from the
// upload, so the SDK does not have to buffer the stream to discover it.
func (m *MinIO) PutObject(
	ctx context.Context,
	bucket, key string,
	reader io.Reader,
	size int64,
	contentType string,
) error {
	_, err := m.Client.PutObject(ctx, bucket, key, reader, size, miniogo.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("minio - PutObject - m.Client.PutObject: %w", err)
	}

	return nil
}

// PresignedGetObject returns a time-limited link that hands the object to the
// browser as a download under the name the file carries. The link is signed
// offline, so no request reaches the bucket here.
func (m *MinIO) PresignedGetObject(
	ctx context.Context,
	bucket, key string,
	expiry time.Duration,
	downloadName string,
) (string, error) {
	params := url.Values{}

	if downloadName != "" {
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": downloadName})
		if disposition != "" {
			params.Set("response-content-disposition", disposition)
		}
	}

	link, err := m.presignClient.PresignedGetObject(ctx, bucket, key, expiry, params)
	if err != nil {
		return "", fmt.Errorf("minio - PresignedGetObject - m.presignClient.PresignedGetObject: %w", err)
	}

	return link.String(), nil
}

// RemoveObject deletes one object from the bucket. Deleting a key that is
// already gone succeeds, so cleaning up twice is safe.
func (m *MinIO) RemoveObject(ctx context.Context, bucket, key string) error {
	err := m.Client.RemoveObject(ctx, bucket, key, miniogo.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio - RemoveObject - m.Client.RemoveObject: %w", err)
	}

	return nil
}

// GetObject opens one object for reading. The caller closes the returned
// reader when it is done.
func (m *MinIO) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	object, err := m.Client.GetObject(ctx, bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio - GetObject - m.Client.GetObject: %w", err)
	}

	if _, err := object.Stat(); err != nil {
		_ = object.Close()

		return nil, fmt.Errorf("minio - GetObject - Stat: %w", err)
	}

	return object, nil
}

// CopyObject duplicates one object into another key in the same bucket. It is
// a server-side copy, so the bytes never leave the bucket.
func (m *MinIO) CopyObject(ctx context.Context, bucket, dstKey, srcKey string) error {
	_, err := m.Client.CopyObject(ctx, miniogo.CopyDestOptions{
		Bucket: bucket,
		Object: dstKey,
	}, miniogo.CopySrcOptions{
		Bucket: bucket,
		Object: srcKey,
	})
	if err != nil {
		return fmt.Errorf("minio - CopyObject - m.Client.CopyObject: %w", err)
	}

	return nil
}

// CreateMultipartUpload opens a multipart upload and returns the id the
// following part calls carry. Nothing is visible in the bucket until the
// upload completes.
func (m *MinIO) CreateMultipartUpload(
	ctx context.Context,
	bucket, key, contentType string,
) (string, error) {
	uploadID, err := m.core().NewMultipartUpload(ctx, bucket, key, miniogo.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("minio - CreateMultipartUpload - core.NewMultipartUpload: %w", err)
	}

	return uploadID, nil
}

// UploadPart writes one numbered part of an open upload and returns the ETag
// the bucket assigned to it. The caller keeps the ETag for the completion.
func (m *MinIO) UploadPart(
	ctx context.Context,
	bucket, key, uploadID string,
	partNumber int,
	reader io.Reader,
	size int64,
) (string, error) {
	part, err := m.core().PutObjectPart(
		ctx,
		bucket,
		key,
		uploadID,
		partNumber,
		reader,
		size,
		miniogo.PutObjectPartOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("minio - UploadPart - core.PutObjectPart: %w", err)
	}

	return part.ETag, nil
}

// CompleteMultipartUpload joins the uploaded parts into one object. The parts
// are sent in number order, whatever order the caller collected them in.
func (m *MinIO) CompleteMultipartUpload(
	ctx context.Context,
	bucket, key, uploadID string,
	parts []multipart.Part,
) error {
	_, err := m.core().CompleteMultipartUpload(
		ctx,
		bucket,
		key,
		uploadID,
		completeParts(parts),
		miniogo.PutObjectOptions{},
	)
	if err != nil {
		return fmt.Errorf("minio - CompleteMultipartUpload - core.CompleteMultipartUpload: %w", err)
	}

	return nil
}

// AbortMultipartUpload discards an open upload and the parts already sent. An
// upload that is already gone counts as aborted, so a sweep can run twice.
func (m *MinIO) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	err := m.core().AbortMultipartUpload(ctx, bucket, key, uploadID)
	if err == nil {
		return nil
	}

	if miniogo.ToErrorResponse(err).Code == miniogo.NoSuchUpload {
		return nil
	}

	return fmt.Errorf("minio - AbortMultipartUpload - core.AbortMultipartUpload: %w", err)
}

// core exposes the low level multipart calls of the SDK client.
func (m *MinIO) core() miniogo.Core {
	return miniogo.Core{Client: m.Client}
}

// completeParts converts the neutral parts into the SDK shape in ascending
// part-number order, which is the order the bucket expects.
func completeParts(parts []multipart.Part) []miniogo.CompletePart {
	ordered := slices.Clone(parts)

	slices.SortFunc(ordered, func(a, b multipart.Part) int {
		return cmp.Compare(a.Number, b.Number)
	})

	converted := make([]miniogo.CompletePart, len(ordered))
	for i, part := range ordered {
		converted[i] = miniogo.CompletePart{PartNumber: part.Number, ETag: part.ETag}
	}

	return converted
}
