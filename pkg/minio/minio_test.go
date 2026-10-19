package minio

import (
	"context"
	"errors"
	"testing"

	miniogo "github.com/minio/minio-go/v7"
)

// errBucketFailed is the failure the fake client reports.
var errBucketFailed = errors.New("bucket failed")

// fakeBucketClient is a MinIO bucket API that answers from a script.
type fakeBucketClient struct {
	exists        bool
	existsAfter   bool
	existsErr     error
	makeErr       error
	existsCalls   int
	madeBucketIDs []string
}

func (f *fakeBucketClient) BucketExists(_ context.Context, _ string) (bool, error) {
	f.existsCalls++

	if f.existsErr != nil {
		return false, f.existsErr
	}

	if f.existsCalls == 1 {
		return f.exists, nil
	}

	return f.existsAfter, nil
}

func (f *fakeBucketClient) MakeBucket(_ context.Context, bucketName string, _ miniogo.MakeBucketOptions) error {
	if f.makeErr != nil {
		return f.makeErr
	}

	f.madeBucketIDs = append(f.madeBucketIDs, bucketName)

	return nil
}

func TestEnsureBucketCreatesTheMissingBucket(t *testing.T) {
	t.Parallel()

	client := &fakeBucketClient{}
	m := &MinIO{buckets: client, region: "us-east-1"}

	err := m.EnsureBucket(context.Background(), "files")
	if err != nil {
		t.Fatalf("EnsureBucket() error = %v, want nil", err)
	}

	if len(client.madeBucketIDs) != 1 || client.madeBucketIDs[0] != "files" {
		t.Errorf("MakeBucket() called with %v, want [files]", client.madeBucketIDs)
	}
}

func TestEnsureBucketKeepsTheExistingBucket(t *testing.T) {
	t.Parallel()

	client := &fakeBucketClient{exists: true}
	m := &MinIO{buckets: client, region: "us-east-1"}

	err := m.EnsureBucket(context.Background(), "files")
	if err != nil {
		t.Fatalf("EnsureBucket() error = %v, want nil", err)
	}

	if len(client.madeBucketIDs) != 0 {
		t.Errorf("MakeBucket() called with %v, want no call", client.madeBucketIDs)
	}
}

func TestEnsureBucketFailsWhenTheBucketCannotBeCreated(t *testing.T) {
	t.Parallel()

	client := &fakeBucketClient{makeErr: errBucketFailed}
	m := &MinIO{buckets: client, region: "us-east-1"}

	err := m.EnsureBucket(context.Background(), "files")
	if !errors.Is(err, errBucketFailed) {
		t.Fatalf("EnsureBucket() error = %v, want %v", err, errBucketFailed)
	}
}

func TestEnsureBucketAcceptsTheBucketCreatedByAnotherInstance(t *testing.T) {
	t.Parallel()

	client := &fakeBucketClient{makeErr: errBucketFailed, existsAfter: true}
	m := &MinIO{buckets: client, region: "us-east-1"}

	err := m.EnsureBucket(context.Background(), "files")
	if err != nil {
		t.Fatalf("EnsureBucket() error = %v, want nil", err)
	}
}

func TestEnsureBucketFailsWhenTheBucketCannotBeChecked(t *testing.T) {
	t.Parallel()

	client := &fakeBucketClient{existsErr: errBucketFailed}
	m := &MinIO{buckets: client, region: "us-east-1"}

	err := m.EnsureBucket(context.Background(), "files")
	if !errors.Is(err, errBucketFailed) {
		t.Fatalf("EnsureBucket() error = %v, want %v", err, errBucketFailed)
	}
}
