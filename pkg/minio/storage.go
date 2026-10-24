package minio

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"time"

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
