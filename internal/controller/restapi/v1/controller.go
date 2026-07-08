package v1

import (
	"github.com/go-playground/validator/v10"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// V1 -.
type V1 struct {
	t  usecase.Translation
	u  usecase.User
	tk usecase.Task
	l  logger.Interface
	v  *validator.Validate
}
