package request

import "strings"

// UploadStart opens one chunked upload. The browser sends the size up front so
// the server can reserve it and plan the parts before any byte arrives.
type UploadStart struct {
	FolderID    string `json:"folder"       validate:"required,uuid4"`
	Name        string `json:"name"         validate:"required,max=255"`
	Size        int64  `json:"size"         validate:"gte=0"`
	ContentType string `json:"content_type" validate:"max=255"`
}

// Sanitize trims the name before validation runs. The rest of the name rules —
// no slashes, no control characters — belong to the domain.
func (r *UploadStart) Sanitize() {
	r.Name = strings.TrimSpace(r.Name)
}
