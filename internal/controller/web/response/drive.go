package response

import (
	"fmt"
	"strconv"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

const (
	// _driveDateLayout is the date and time every drive row prints for the
	// moment it last changed.
	_driveDateLayout = "2006-01-02 15:04"

	// _rootCrumbIndex is the position of the root of the drive in a
	// breadcrumb: the path is always root first.
	_rootCrumbIndex = 0

	// _folderIndentPx is the left padding per level of depth in a folder
	// destination select, so the tree shape is visible.
	_folderIndentPx = 24
)

// Crumb is one step of the breadcrumb above a folder listing.
type Crumb struct {
	Name    string
	Href    string
	Current bool
}

// FolderRow is one folder in the drive table. The item count is how many
// entries sit directly inside it, already worded for the cell.
type FolderRow struct {
	ID      string
	Name    string
	Href    string
	Items   string
	Updated string
}

// FileRow is one file in the drive table. The size is the number of bytes,
// already worded for the cell.
type FileRow struct {
	ID      string
	Name    string
	Type    string
	Size    string
	Updated string
}

// Drive is everything the drive page renders for one folder.
type Drive struct {
	Title      string
	FolderID   string
	FolderName string
	IsRoot     bool
	Breadcrumb []Crumb
	Folders    []FolderRow
	Files      []FileRow
}

// FileInfo is the file drawer: what the file is, and the two actions it
// offers.
type FileInfo struct {
	ID      string
	Name    string
	Type    string
	Size    string
	Updated string
}

// NewDrive maps the folder view to the page the drive template renders. The
// root crumb points at the home page; every other crumb points at the folder
// route, so a bookmark of any step opens that step.
func NewDrive(view *entity.FolderView) Drive {
	crumbs := make([]Crumb, 0, len(view.Breadcrumb))

	for i, ref := range view.Breadcrumb {
		href := "/folders/" + ref.ID
		if i == _rootCrumbIndex {
			href = "/"
		}

		crumbs = append(crumbs, Crumb{
			Name:    ref.Name,
			Href:    href,
			Current: i == len(view.Breadcrumb)-1,
		})
	}

	folders := make([]FolderRow, 0, len(view.Folders))
	for i := range view.Folders {
		item := &view.Folders[i]
		folders = append(folders, FolderRow{
			ID:      item.ID,
			Name:    item.Name,
			Href:    "/folders/" + item.ID,
			Items:   itemLabel(item.ItemCount),
			Updated: item.UpdatedAt.Format(_driveDateLayout),
		})
	}

	files := make([]FileRow, 0, len(view.Files))
	for i := range view.Files {
		item := &view.Files[i]
		files = append(files, FileRow{
			ID:      item.ID,
			Name:    item.Name,
			Type:    item.ContentType,
			Size:    byteLabel(item.Size),
			Updated: item.UpdatedAt.Format(_driveDateLayout),
		})
	}

	return Drive{
		FolderID:   view.Folder.ID,
		FolderName: view.Folder.Name,
		IsRoot:     view.Folder.IsRoot(),
		Breadcrumb: crumbs,
		Folders:    folders,
		Files:      files,
	}
}

// NewFileInfo maps the file behind a drawer to the type the template renders.
func NewFileInfo(file *entity.File) FileInfo {
	return FileInfo{
		ID:      file.ID,
		Name:    file.Name,
		Type:    file.ContentType,
		Size:    byteLabel(file.Size),
		Updated: file.UpdatedAt.Format(_driveDateLayout),
	}
}

// byteLabel words a size in bytes, which is the unit the drive shows.
func byteLabel(size int64) string {
	return strconv.FormatInt(size, 10) + " bytes"
}

// itemLabel words the number of entries a folder holds.
func itemLabel(count int) string {
	if count == 1 {
		return "1 item"
	}

	return fmt.Sprintf("%d items", count)
}

// FolderOption is one folder in a destination select. Depth is the number of
// ancestors above it, so the tree shape shows as indentation in a flat list.
type FolderOption struct {
	ID    string
	Name  string
	Depth int
	Style string // inline style, e.g. "padding-left: 48px"
}

// FolderOptions maps a flat list of folders to select options, computing the
// depth of each one from its parent chain so the tree shape is visible.
func FolderOptions(folders []entity.Folder) []FolderOption {
	byID := make(map[string]entity.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}

	depth := make(map[string]int)

	var depthOf func(id string) int

	depthOf = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}

		f, ok := byID[id]
		if !ok || f.ParentID == nil {
			depth[id] = 0

			return 0
		}

		depth[id] = 1 + depthOf(*f.ParentID)

		return depth[id]
	}

	opts := make([]FolderOption, 0, len(folders))
	for _, f := range folders {
		d := depthOf(f.ID)
		opts = append(opts, FolderOption{
			ID:    f.ID,
			Name:  f.Name,
			Depth: d,
			Style: fmt.Sprintf("padding-left: %dpx", d*_folderIndentPx),
		})
	}

	return opts
}
