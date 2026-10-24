package request

import "strings"

// FolderCreate is the drawer form that makes a folder inside the folder on the
// screen. The parent travels as the hidden field the page renders.
type FolderCreate struct {
	ParentID string `form:"folder" validate:"required,uuid4"`
	Name     string `form:"name"   validate:"required,max=255"`
}

// Sanitize trims the name before validation runs. The rest of the name rules —
// no slashes, no control characters — belong to the domain.
func (r *FolderCreate) Sanitize() {
	r.Name = strings.TrimSpace(r.Name)
}

// FileUpload is the drawer form that stores one file. The file itself is read
// from the multipart request; this type carries the fields next to it.
type FileUpload struct {
	FolderID string `form:"folder" validate:"required,uuid4"`
	Name     string `form:"name"   validate:"required,max=255"`
}

// Sanitize trims the name before validation runs.
func (r *FileUpload) Sanitize() {
	r.Name = strings.TrimSpace(r.Name)
}

// FileRename is the form the file drawer submits to change the name a file is
// listed under.
type FileRename struct {
	Name string `form:"name" validate:"required,max=255"`
}

// Sanitize trims the name before validation runs.
func (r *FileRename) Sanitize() {
	r.Name = strings.TrimSpace(r.Name)
}

// FolderRename is the form the folder drawer submits to change the name a
// folder is listed under.
type FolderRename struct {
	Name string `form:"name" validate:"required,max=255"`
}

// Sanitize trims the name before validation runs.
func (r *FolderRename) Sanitize() {
	r.Name = strings.TrimSpace(r.Name)
}
