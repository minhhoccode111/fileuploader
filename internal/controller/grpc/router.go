package grpc

import (
	v1 "github.com/minhhoccode111/fileuploader/internal/controller/grpc/v1"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	pbgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NewRouter -.
func NewRouter(app *pbgrpc.Server, t usecase.Translation, u usecase.User, tk usecase.Task, l logger.Interface) {
	{
		v1.NewAuthRoutes(app, u, l)
		v1.NewTaskRoutes(app, tk, l)
		v1.NewTranslationRoutes(app, t, l)
	}

	reflection.Register(app)
}
