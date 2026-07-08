package v1

import (
	v1 "github.com/minhhoccode111/fileuploader/internal/controller/nats_rpc/v1"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/jwt"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/nats/nats_rpc/server"
)

// NewRouter -.
func NewRouter(t usecase.Translation, u usecase.User, tk usecase.Task, j *jwt.Manager, l logger.Interface) map[string]server.CallHandler {
	routes := make(map[string]server.CallHandler)

	{
		v1.NewRoutes(routes, t, u, tk, j, l)
	}

	return routes
}
