package response

import "github.com/minhhoccode111/fileuploader/internal/entity"

// TaskList -.
type TaskList struct {
	Tasks []entity.Task `json:"tasks"`
	Total int           `json:"total" example:"42"`
} // @name v1.TaskList
