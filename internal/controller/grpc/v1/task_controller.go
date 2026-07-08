package v1

import (
	"github.com/go-playground/validator/v10"
	v1 "github.com/minhhoccode111/fileuploader/docs/proto/v1"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// TaskController -.
type TaskController struct {
	v1.UnimplementedTaskServiceServer

	tk usecase.Task
	l  logger.Interface
	v  *validator.Validate
}
