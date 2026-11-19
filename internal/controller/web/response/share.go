package response

import (
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

const _shareDateLayout = "2006-01-02 15:04"

// ShareRow is one active share in the index: its token, the folder it opens,
// and when it stops being valid.
type ShareRow struct {
	ID         string
	FolderID   string
	FolderName string
	Expires    string // "Unlimited" or formatted time
	ExpiresAt  *time.Time
	Link       string
}

// NewShareRow maps a share and its folder name to the row the index renders.
func NewShareRow(share *entity.Share, folderName string) ShareRow {
	expires := "Unlimited"
	if share.ExpiresAt != nil {
		expires = share.ExpiresAt.Format(_shareDateLayout)
	}

	return ShareRow{
		ID:         share.ID,
		FolderID:   share.FolderID,
		FolderName: folderName,
		Expires:    expires,
		ExpiresAt:  share.ExpiresAt,
		Link:       "/share/" + share.ID,
	}
}

// SharedDrive is the read-only listing a public visitor sees. It reuses the
// drive shape, but its links point at the share routes.
type SharedDrive struct {
	ShareID    string
	Title      string
	FolderID   string
	FolderName string
	IsRoot     bool
	Breadcrumb []Crumb
	Folders    []SharedFolderRow
	Files      []SharedFileRow
}

type SharedFolderRow struct {
	ID      string
	Name    string
	Href    string
	Items   string
	Updated string
}

type SharedFileRow struct {
	ID       string
	Name     string
	Type     string
	Size     string
	Updated  string
	Download string
}

// NewSharedDrive maps a folder view to the public share page. The root crumb
// points back at the share root; folder rows point at nested share routes.
func NewSharedDrive(shareID string, view *entity.FolderView) SharedDrive {
	crumbs := make([]Crumb, 0, len(view.Breadcrumb))

	for i, ref := range view.Breadcrumb {
		href := "/share/" + shareID + "/folders/" + ref.ID
		if i == _rootCrumbIndex {
			href = "/share/" + shareID
		}

		crumbs = append(crumbs, Crumb{
			Name:    ref.Name,
			Href:    href,
			Current: i == len(view.Breadcrumb)-1,
		})
	}

	folders := make([]SharedFolderRow, 0, len(view.Folders))
	for i := range view.Folders {
		item := &view.Folders[i]
		folders = append(folders, SharedFolderRow{
			ID:      item.ID,
			Name:    item.Name,
			Href:    "/share/" + shareID + "/folders/" + item.ID,
			Items:   itemLabel(item.ItemCount),
			Updated: item.UpdatedAt.Format(_driveDateLayout),
		})
	}

	files := make([]SharedFileRow, 0, len(view.Files))
	for i := range view.Files {
		item := &view.Files[i]
		files = append(files, SharedFileRow{
			ID:       item.ID,
			Name:     item.Name,
			Type:     item.ContentType,
			Size:     ByteLabel(item.Size),
			Updated:  item.UpdatedAt.Format(_driveDateLayout),
			Download: "/share/" + shareID + "/files/" + item.ID + "/download",
		})
	}

	return SharedDrive{
		ShareID:    shareID,
		FolderID:   view.Folder.ID,
		FolderName: view.Folder.Name,
		IsRoot:     view.Folder.IsRoot(),
		Breadcrumb: crumbs,
		Folders:    folders,
		Files:      files,
	}
}
