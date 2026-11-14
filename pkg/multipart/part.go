// Package multipart carries the storage-neutral description of one part of a
// multipart upload.
package multipart

// Part is one uploaded part and the ETag the storage returned for it.
type Part struct {
	Number int
	ETag   string
}
