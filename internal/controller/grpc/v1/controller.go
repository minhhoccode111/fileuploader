package v1

import (
	"github.com/go-playground/validator/v10"
	v1 "github.com/minhhoccode111/fileuploader/docs/proto/v1"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// TranslationController -.
type TranslationController struct {
	v1.UnimplementedTranslationServer

	t usecase.Translation
	l logger.Interface
	v *validator.Validate
}
