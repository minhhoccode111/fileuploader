package response

import (
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// The drawer keeps the raw byte count beside the worded size, so the download
// link can decide whether the page may buffer the file or must hand it to the
// browser.
func TestNewFileInfoKeepsTheRawByteCount(t *testing.T) {
	t.Parallel()

	file := entity.File{
		ID:   "file",
		Name: "holiday.mp4",
		Size: 7 << 20,
	}

	info := NewFileInfo(&file)

	if info.Bytes != file.Size {
		t.Errorf("NewFileInfo() bytes = %d, want %d", info.Bytes, file.Size)
	}

	if info.Size != ByteLabel(file.Size) {
		t.Errorf("NewFileInfo() size = %q, want %q", info.Size, ByteLabel(file.Size))
	}
}
