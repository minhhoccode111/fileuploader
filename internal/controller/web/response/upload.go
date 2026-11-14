package response

// UploadStart is the plan of one chunked upload: the session the parts name,
// the size every part carries, and how many parts the file needs. The last part
// may be shorter.
type UploadStart struct {
	UploadID  string `json:"upload_id"`
	PartSize  int64  `json:"part_size"`
	PartCount int    `json:"part_count"`
}
