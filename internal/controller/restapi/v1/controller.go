package v1

import (
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/go-playground/validator/v10"
)

// V1 -.
type V1 struct {
	t  usecase.Translation
	u  usecase.User
	tk usecase.Task
	l  logger.Interface
	v  *validator.Validate
}
