package entity

import "time"

const (
	// MinUploadPartBytes is the smallest part an S3-compatible store accepts,
	// apart from the last part of a multipart upload, which may be shorter.
	MinUploadPartBytes int64 = 5 << 20

	// MaxUploadParts is the largest number of parts one multipart upload may
	// carry. A file over that limit is refused before any byte is stored.
	MaxUploadParts = 10000
)

// UploadSession is one multipart upload in flight. The rows of the parts live
// next to it in the store; the session holds what the server needs to decide
// whether the next request is still valid. ExpiresAt is pushed forward by
// every accepted part, so an abandoned upload is swept once the account stops
// touching it.
type UploadSession struct {
	ID          string
	UserID      string
	FolderID    string
	FileID      string
	Name        string
	ContentType string
	StorageKey  string
	MultipartID string
	Size        int64
	PartSize    int64
	PartCount   int
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// UploadPartCount reports how many parts a file of the given size needs at the
// given part size. The last part may be shorter than the others. A size of
// zero needs no part at all.
func UploadPartCount(size, partSize int64) (int, error) {
	if size < 0 || partSize < MinUploadPartBytes {
		return 0, ErrInvalidUpload
	}

	if size == 0 {
		return 0, nil
	}

	count := 1 + (size-1)/partSize
	if count > MaxUploadParts {
		return 0, ErrInvalidUpload
	}

	return int(count), nil
}

// ExpectedPartSize reports the size that part partNumber must have. Every
// part but the last one is PartSize bytes long; the last one carries whatever
// the file has left. A number outside 1..PartCount is a mismatch, because the
// upload no longer matches the session it was created against.
func (s *UploadSession) ExpectedPartSize(partNumber int) (int64, error) {
	if partNumber < 1 || partNumber > s.PartCount {
		return 0, ErrUploadPartMismatch
	}

	if partNumber < s.PartCount {
		return s.PartSize, nil
	}

	return s.Size - int64(s.PartCount-1)*s.PartSize, nil
}
