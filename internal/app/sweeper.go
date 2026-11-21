package app

import (
	"context"
	"fmt"
	"time"

	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// _uploadSweepInterval is how often an idle upload session is looked for.
const _uploadSweepInterval = 5 * time.Minute

// uploadSweeper releases the upload sessions no account finished.
type uploadSweeper interface {
	SweepUploads(ctx context.Context) (int, error)
}

// startUploadSweeper runs one sweep now and then one every _uploadSweepInterval
// until ctx is done. It returns at once: the loop lives in its own goroutine,
// so it neither delays startup nor blocks shutdown.
func startUploadSweeper(ctx context.Context, files uploadSweeper, l logger.Interface) {
	go func() {
		sweepUploads(ctx, files, l)

		ticker := time.NewTicker(_uploadSweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepUploads(ctx, files, l)
			}
		}
	}()
}

// sweepUploads runs one pass and logs what it released.
func sweepUploads(ctx context.Context, files uploadSweeper, l logger.Interface) {
	count, err := files.SweepUploads(ctx)
	if err != nil {
		// A sweep cut short by shutdown is not a failure.
		if ctx.Err() != nil {
			return
		}

		l.Error(fmt.Errorf("app - sweepUploads - files.SweepUploads: %w", err))

		return
	}

	l.Info("app - sweepUploads: released %d uploads", count)
}
