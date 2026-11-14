package minio

import (
	"testing"

	"github.com/minhhoccode111/fileuploader/pkg/multipart"
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
