package response

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

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

	// _byteUnit is the number of bytes in one kilobyte of the binary units
	// the drive shows.
	_byteUnit = 1 << 10

	// _byteLabelSmall is the size below which a value is printed in bytes
	// without a fractional part.
	_byteLabelSmall = 10

	// _homeLabel words the root of the drive in a result location.
	_homeLabel = "Home"

	// _rootName is the name the root folder carries in the database. It reads
	// as Home on a page.
	_rootName = "/"

	// _homeHref is the address of the root folder page.
	_homeHref = "/"

	// _searchHref is the address of the whole-drive search page.
	_searchHref = "/search"

	// _searchEntriesHref is the partial route that answers the load-more
	// sentinel of a search.
	_searchEntriesHref = "/htmx/search"

	// _pathSeparator joins the names of a result location.
	_pathSeparator = " / "
)

// Crumb is one step of the breadcrumb above a folder listing.
type Crumb struct {
	Name    string
	Href    string
	Current bool
}

// FolderRow is one folder in the drive table. The item count is how many
// entries sit directly inside it, already worded for the cell. Path and
// PathHref carry the location of a whole-drive search result: the chain from
// the root, and the folder the chain ends at.
type FolderRow struct {
	ID       string
	Name     string
	Href     string
	Items    string
	Updated  string
	Path     string
	PathHref string
}

// FileRow is one file in the drive table. The size is the number of bytes,
// already worded for the cell. Path and PathHref carry the location of a
// whole-drive search result: the chain from the root, and the folder that holds
// the file.
type FileRow struct {
	ID       string
	Name     string
	Type     string
	Size     string
	Updated  string
	Path     string
	PathHref string
}

// Drive is everything the drive page renders for one folder. Search is the
// term the listing was filtered with, SearchAction is where the search form of
// the page submits, and NextURL is the partial route that answers the
// load-more sentinel, empty when the listing is complete.
//
// The flags mark the whole-drive search, a state of its own: its rows are
// read-only, so a file name downloads instead of opening the action drawer,
// and every row shows the folder it lives in.
type Drive struct {
	Title        string
	FolderID     string
	FolderName   string
	IsRoot       bool
	Breadcrumb   []Crumb
	Folders      []FolderRow
	Files        []FileRow
	Search       string
	SearchAction string
	NextURL      string
	SearchMode   bool
	ShowPath     bool
	ShortSearch  bool
	From         string
	ClearHref    string
}

// FileInfo is the file drawer: what the file is, and the two actions it
// offers. Bytes is the raw size, so the download link can choose between the
// progress bar and the browser's own download without parsing the worded size.
type FileInfo struct {
	ID      string
	Name    string
	Type    string
	Size    string
	Bytes   int64
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
			Size:    ByteLabel(item.Size),
			Updated: item.UpdatedAt.Format(_driveDateLayout),
		})
	}

	return Drive{
		FolderID:     view.Folder.ID,
		FolderName:   view.Folder.Name,
		IsRoot:       view.Folder.IsRoot(),
		Breadcrumb:   crumbs,
		Folders:      folders,
		Files:        files,
		Search:       view.Search,
		SearchAction: searchAction(&view.Folder),
		NextURL:      nextEntryURL(view.Folder.ID, view.Search, view.Next),
	}
}

// searchAction is where the search form of a page submits: the folder route of
// the page, or Home for the root folder, which keeps its own address. The scope
// control of the form decides whether the submit stays on this page or leaves
// for the whole-drive search.
func searchAction(folder *entity.Folder) string {
	if folder.IsRoot() {
		return "/"
	}

	return "/folders/" + folder.ID
}

// NewSearch maps a whole-drive search view to the page the search template
// renders. The breadcrumb is a single current Search crumb, because the page is
// a state of its own rather than a folder. Every row carries the chain from the
// root to its folder, so a result found by search can be reached where it
// lives.
func NewSearch(view *entity.SearchView) Drive {
	crumbs := []Crumb{{Name: "Search", Href: _searchHref, Current: true}}

	folders := make([]FolderRow, 0, len(view.Folders))
	for i := range view.Folders {
		item := &view.Folders[i]
		folders = append(folders, FolderRow{
			ID:       item.ID,
			Name:     item.Name,
			Href:     "/folders/" + item.ID,
			Items:    itemLabel(item.ItemCount),
			Updated:  item.UpdatedAt.Format(_driveDateLayout),
			Path:     pathLabel(item.Path),
			PathHref: "/folders/" + item.ID,
		})
	}

	files := make([]FileRow, 0, len(view.Files))
	for i := range view.Files {
		item := &view.Files[i]
		files = append(files, FileRow{
			ID:       item.ID,
			Name:     item.Name,
			Type:     item.ContentType,
			Size:     ByteLabel(item.Size),
			Updated:  item.UpdatedAt.Format(_driveDateLayout),
			Path:     pathLabel(item.Path),
			PathHref: "/folders/" + item.FolderID,
		})
	}

	return Drive{
		Breadcrumb:   crumbs,
		Folders:      folders,
		Files:        files,
		Search:       view.Search,
		SearchAction: _searchHref,
		NextURL:      searchNextURL(view.Search, view.From, view.Next),
		SearchMode:   true,
		ShowPath:     true,
		ShortSearch:  view.TooShort,
		From:         view.From,
		ClearHref:    searchClearHref(view.From),
	}
}

// pathLabel words the chain from the root to a result's folder. The root folder
// carries "/" as its name, which reads as Home; the rest join with a simple
// separator, which is safe because a stored name cannot hold one.
func pathLabel(path []entity.FolderRef) string {
	if len(path) == 0 {
		return _homeLabel
	}

	names := make([]string, 0, len(path))
	for _, ref := range path {
		name := ref.Name
		if name == _rootName {
			name = _homeLabel
		}

		names = append(names, name)
	}

	return strings.Join(names, _pathSeparator)
}

// searchClearHref is where the Clear link of the search page returns to: the
// folder the search came from, or Home when it named none.
func searchClearHref(from string) string {
	if from == "" {
		return _homeHref
	}

	return "/folders/" + from
}

// nextEntryURL is the partial route that answers the load-more sentinel of a
// folder listing: the batch after the cursor, under the same search. Both
// values are escaped, and an empty search is left out, so the link stays short.
func nextEntryURL(folderID, search string, after *entity.EntryCursor) string {
	query := entryQuery(search, after)
	if query == nil {
		return ""
	}

	return "/htmx/folders/" + folderID + "/entries?" + query.Encode()
}

// searchNextURL is the partial route that answers the load-more sentinel of a
// search: the batch after the cursor, under the same term and the same source
// folder. An empty value is left out, so the link stays short.
func searchNextURL(search, from string, after *entity.EntryCursor) string {
	query := entryQuery(search, after)
	if query == nil {
		return ""
	}

	if from != "" {
		query.Set("from", from)
	}

	return _searchEntriesHref + "?" + query.Encode()
}

// entryQuery is the query of a next-page link: the cursor the listing resumes
// after and the search it runs under. A nil result means the listing is
// complete, so there is no link to carry.
func entryQuery(search string, after *entity.EntryCursor) url.Values {
	if after == nil {
		return nil
	}

	query := url.Values{}
	query.Set("after", after.Encode())

	if search != "" {
		query.Set("q", search)
	}

	return query
}

// NewFileInfo maps the file behind a drawer to the type the template renders.
func NewFileInfo(file *entity.File) FileInfo {
	return FileInfo{
		ID:      file.ID,
		Name:    file.Name,
		Type:    file.ContentType,
		Size:    ByteLabel(file.Size),
		Bytes:   file.Size,
		Updated: file.UpdatedAt.Format(_driveDateLayout),
	}
}

// byteLabel words a size in bytes using the largest unit that keeps the value
// readable, so the drive shows "1.5 KB" instead of "1536 bytes".
func byteLabel(size int64) string {
	if size < _byteLabelSmall {
		return strconv.FormatInt(size, 10) + " B"
	}

	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

	exponent := max(int(math.Floor(math.Log(float64(size))/math.Log(_byteUnit))), 0)
	if exponent >= len(units) {
		exponent = len(units) - 1
	}

	value := float64(size) / math.Pow(_byteUnit, float64(exponent))

	// Rounding must not print the next unit's base (e.g. "1024 KB"
	// instead of "1.0 MB").
	if exponent < len(units)-1 {
		var rounded float64
		if value < _byteLabelSmall {
			rounded = math.Round(value*float64(_byteLabelSmall)) / float64(_byteLabelSmall)
		} else {
			rounded = math.Round(value)
		}

		if rounded >= _byteUnit {
			exponent++
			value /= _byteUnit
		}
	}

	if value < _byteLabelSmall {
		return fmt.Sprintf("%.1f %s", value, units[exponent])
	}

	return fmt.Sprintf("%.0f %s", value, units[exponent])
}

// ByteLabel is the exported name of byteLabel.
func ByteLabel(size int64) string {
	return byteLabel(size)
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

// FolderOptions maps a flat list of folders to select options. The folders are
// ordered as a tree (parents before their children, siblings in name order) so
// the indentation reads as a tree, and the depth of each one is computed from
// its parent chain.
func FolderOptions(folders []entity.Folder) []FolderOption {
	byID := make(map[string]entity.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}

	children := make(map[string][]string)
	roots := make([]string, 0)

	for _, f := range folders {
		if f.ParentID == nil {
			roots = append(roots, f.ID)

			continue
		}

		if _, ok := byID[*f.ParentID]; !ok {
			// The parent is not in the list (for example, it was filtered out
			// by a move). Treat the folder as a root so it still shows up.
			roots = append(roots, f.ID)

			continue
		}

		children[*f.ParentID] = append(children[*f.ParentID], f.ID)
	}

	sortByName := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a, b := byID[ids[i]], byID[ids[j]]

			if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
				return cmp < 0
			}

			return a.ID < b.ID
		})
	}

	sortByName(roots)

	for parent := range children {
		sortByName(children[parent])
	}

	opts := make([]FolderOption, 0, len(folders))

	for _, root := range roots {
		appendFolderTree(&opts, byID, children, root, 0)
	}

	return opts
}

// FolderNode is one folder in the quick-navigate tree, with its children
// already nested so the template can render them recursively.
type FolderNode struct {
	ID       string
	Name     string
	Children []FolderNode
}

// FolderTree turns a flat list of folders into a nested tree, roots first and
// siblings in name order. Used by the quick-navigate view.
func FolderTree(folders []entity.Folder) []FolderNode {
	byID := make(map[string]entity.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}

	children := make(map[string][]string)
	roots := make([]string, 0)

	for _, f := range folders {
		if f.ParentID == nil {
			roots = append(roots, f.ID)

			continue
		}

		if _, ok := byID[*f.ParentID]; !ok {
			roots = append(roots, f.ID)

			continue
		}

		children[*f.ParentID] = append(children[*f.ParentID], f.ID)
	}

	sortByName := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a, b := byID[ids[i]], byID[ids[j]]

			if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
				return cmp < 0
			}

			return a.ID < b.ID
		})
	}

	sortByName(roots)

	for parent := range children {
		sortByName(children[parent])
	}

	var build func(id string) FolderNode

	build = func(id string) FolderNode {
		node := FolderNode{ID: id, Name: byID[id].Name}

		for _, child := range children[id] {
			node.Children = append(node.Children, build(child))
		}

		return node
	}

	out := make([]FolderNode, 0, len(roots))

	for _, root := range roots {
		out = append(out, build(root))
	}

	return out
}

// appendFolderTree appends one folder and its descendants to opts in tree order.
func appendFolderTree(
	opts *[]FolderOption,
	byID map[string]entity.Folder,
	children map[string][]string,
	id string,
	depth int,
) {
	f := byID[id]

	*opts = append(*opts, FolderOption{
		ID:    f.ID,
		Name:  f.Name,
		Depth: depth,
		Style: fmt.Sprintf("padding-left: %dpx", depth*_folderIndentPx),
	})

	for _, child := range children[id] {
		appendFolderTree(opts, byID, children, child, depth+1)
	}
}
