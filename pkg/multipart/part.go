// Package multipart carries the storage-neutral description of one part of a
// multipart upload.
package multipart

import "time"

// Part is one uploaded part and the ETag the storage returned for it.
type Part struct {
	Number int
	ETag   string
}

// IncompleteUpload is a multipart upload the bucket still holds: the key it
// would become, the upload ID that would join it, and when it started.
type IncompleteUpload struct {
	Key       string
	UploadID  string
	Initiated time.Time
}
