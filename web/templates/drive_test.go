package templates

import (
	"bytes"
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
)

// The drawer hands the raw byte count to the download link, so the page can
// decide between its progress bar and the browser's own download.
func TestFileDrawerPassesTheRawSizeToTheDownloadLink(t *testing.T) {
	t.Parallel()

	const size = int64(42) << 20

	info := response.FileInfo{
		ID:    "file",
		Name:  "big.iso",
		Type:  "application/octet-stream",
		Size:  "42 MB",
		Bytes: size,
	}

	var buf bytes.Buffer

	err := FileDrawerContent(info).Render(t.Context(), &buf)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	html := buf.String()

	if !strings.Contains(html, `data-download-size="44040192"`) {
		t.Errorf("FileDrawerContent() = %s, want the raw size on the download link", html)
	}

	if !strings.Contains(
		html,
		"downloadWithProgress(this.href, this.dataset.downloadName, this.dataset.downloadSize)",
	) {
		t.Error("FileDrawerContent() did not pass the raw size to downloadWithProgress")
	}
}
