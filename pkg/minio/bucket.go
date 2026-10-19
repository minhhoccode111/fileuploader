package minio

import (
	"context"
	"fmt"

	miniogo "github.com/minio/minio-go/v7"
)

// bucketClient is the bucket part of the MinIO API. It is an interface so the
// bootstrap can be tested without a running server.
type bucketClient interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts miniogo.MakeBucketOptions) error
}

// EnsureBucket creates the bucket when it is missing and leaves an existing one
// alone. The app calls it on startup, so a fresh volume gets a usable bucket
// without anyone touching the console.
func (m *MinIO) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := m.buckets.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("minio - EnsureBucket - m.buckets.BucketExists: %w", err)
	}

	if exists {
		return nil
	}

	err = m.buckets.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{Region: m.region})
	if err != nil {
		// Another instance may have created the bucket in the meantime.
		exists, existsErr := m.buckets.BucketExists(ctx, bucket)
		if existsErr == nil && exists {
			return nil
		}

		return fmt.Errorf("minio - EnsureBucket - m.buckets.MakeBucket: %w", err)
	}

	return nil
}
