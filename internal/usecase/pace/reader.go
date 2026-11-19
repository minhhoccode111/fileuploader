// Package pace wraps a stream with the per-account transfer limit. It reads a
// batch at a time, reserves the batch against the account's shared bucket, and
// only then hands the bytes to the caller. The batch keeps the limiter calls
// coarse even when the caller reads in small buffers.
package pace

import (
	"context"
	"io"

	"github.com/minhhoccode111/fileuploader/internal/repo"
)

const (
	// _batchDivisor splits one second of an account's rate into the batches a
	// paced read hands over.
	_batchDivisor = 4

	// _batchFloor is the smallest batch a paced read hands over. Below it the
	// reservations are too fine-grained to matter.
	_batchFloor = int64(64 << 10)

	// _batchCeiling is the largest batch a paced read hands over, so a slow
	// sender is never granted more than a megabyte at once.
	_batchCeiling = int64(1 << 20)
)

// Reader paces the bytes it reads from one stream. It implements io.ReadCloser
// and closes the wrapped stream when it can.
type Reader struct {
	r       io.Reader
	closer  io.Closer
	limiter repo.TransferLimiter
	ctx     context.Context
	userID  string
	scope   repo.TransferScope
	rate    int64
	batch   int
	buf     []byte
	off     int
	nextErr error
}

// NewReader wraps r with the account's transfer limit. A non-positive rate, or
// a missing limiter, returns r unchanged. The caller closes the returned
// reader.
func NewReader(
	ctx context.Context,
	r io.Reader,
	limiter repo.TransferLimiter,
	userID string,
	scope repo.TransferScope,
	rate int64,
) io.ReadCloser {
	if rate <= 0 || limiter == nil {
		return asReadCloser(r)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	reader := &Reader{
		r:       r,
		limiter: limiter,
		ctx:     ctx,
		userID:  userID,
		scope:   scope,
		rate:    rate,
		batch:   BatchSize(rate),
	}

	if closer, ok := r.(io.Closer); ok {
		reader.closer = closer
	}

	return reader
}

// BatchSize returns how many bytes one paced read reserves. It is a quarter of
// a second of the rate, held between the floor and the ceiling.
func BatchSize(rate int64) int {
	batch := min(max(rate/_batchDivisor, _batchFloor), _batchCeiling)

	return int(batch)
}

// Read fills its own batch buffer first, reserves the bytes with the limiter,
// and serves the caller from that buffer. A caller with a small buffer
// therefore still makes one reservation per batch instead of one per read.
func (p *Reader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}

	if p.off == len(p.buf) {
		err := p.fill()
		if err != nil {
			return 0, err
		}
	}

	n := copy(dst, p.buf[p.off:])
	p.off += n

	return n, nil
}

// fill reads one batch from the wrapped stream and reserves it. An error from
// the stream is held until the bytes already read are handed over, so a short
// final read is not lost.
func (p *Reader) fill() error {
	if p.nextErr != nil {
		err := p.nextErr
		p.nextErr = nil

		return err
	}

	if p.buf == nil {
		p.buf = make([]byte, p.batch)
	}

	n, err := p.r.Read(p.buf[:p.batch])
	if n == 0 {
		if err == nil {
			// A reader that makes no progress and reports no error would
			// spin the caller forever.
			return io.ErrNoProgress
		}

		return err
	}

	if paceErr := p.limiter.Pace(p.ctx, p.userID, p.scope, int64(n), p.rate); paceErr != nil {
		return paceErr
	}

	p.buf = p.buf[:n]
	p.off = 0
	p.nextErr = err

	return nil
}

// Close closes the wrapped stream when it can be closed.
func (p *Reader) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}

	return nil
}

// asReadCloser keeps a stream that already closes itself, or gives a plain
// reader the empty Close it needs.
func asReadCloser(r io.Reader) io.ReadCloser {
	if closer, ok := r.(io.ReadCloser); ok {
		return closer
	}

	return io.NopCloser(r)
}
