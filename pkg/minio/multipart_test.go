package minio

import (
	"errors"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/pkg/multipart"
	miniogo "github.com/minio/minio-go/v7"
)

func TestCompletePartsSortsByNumber(t *testing.T) {
	t.Parallel()

	got := completeParts([]multipart.Part{
		{Number: 2, ETag: "two"},
		{Number: 1, ETag: "one"},
	})

	if len(got) != 2 || got[0].PartNumber != 1 || got[1].PartNumber != 2 {
		t.Fatalf("completeParts() = %#v, want parts ordered 1 then 2", got)
	}

	if got[0].ETag != "one" || got[1].ETag != "two" {
		t.Errorf("completeParts() ETags = %q, %q", got[0].ETag, got[1].ETag)
	}
}

// The neutral list keeps what the sweep needs from every upload the bucket
// reports: the key, the upload ID and the initiation time.
func TestCollectIncompleteKeepsTheUploadFields(t *testing.T) {
	t.Parallel()

	initiated := time.Unix(1_760_000_000, 0).UTC()

	infos := make(chan miniogo.ObjectMultipartInfo, 2)
	infos <- miniogo.ObjectMultipartInfo{Key: "users/user/files/one", UploadID: "upload-one", Initiated: initiated}

	infos <- miniogo.ObjectMultipartInfo{Key: "users/user/files/two", UploadID: "upload-two"}

	close(infos)

	got, err := collectIncomplete(infos)
	if err != nil {
		t.Fatalf("collectIncomplete() error = %v, want nil", err)
	}

	want := []multipart.IncompleteUpload{
		{Key: "users/user/files/one", UploadID: "upload-one", Initiated: initiated},
		{Key: "users/user/files/two", UploadID: "upload-two"},
	}

	if len(got) != len(want) {
		t.Fatalf("collectIncomplete() = %d uploads, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("collectIncomplete()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A listing that reports an error is refused as a whole, so the sweep never
// treats a partial list as the bucket.
func TestCollectIncompleteStopsAtTheFirstError(t *testing.T) {
	t.Parallel()

	infos := make(chan miniogo.ObjectMultipartInfo, 2)
	infos <- miniogo.ObjectMultipartInfo{Key: "users/user/files/one", UploadID: "upload-one"}

	infos <- miniogo.ObjectMultipartInfo{Err: errBucketFailed}

	close(infos)

	got, err := collectIncomplete(infos)
	if !errors.Is(err, errBucketFailed) {
		t.Fatalf("collectIncomplete() error = %v, want %v", err, errBucketFailed)
	}

	if got != nil {
		t.Errorf("collectIncomplete() = %v, want no uploads on a failed listing", got)
	}
}
