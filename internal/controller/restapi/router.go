package restapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/config"
	_ "github.com/minhhoccode111/fileuploader/docs" // Swagger docs.
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/middleware"
	v1 "github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/jwt"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	ginprometheus "github.com/zsais/go-gin-prometheus"
)

// NewRouter -.
// Swagger spec:
//
//	@title       File Uploader API
//	@description Multi-domain clean architecture template with translation, user, and task management
//	@version     1.0
//	@host        localhost:8080
//	@BasePath    /v1
//	@securityDefinitions.apikey BearerAuth
//	@in header
//	@name Authorization
func NewRouter(handler *gin.Engine, cfg *config.Config, t usecase.Translation, u usecase.User, tk usecase.Task, jwtManager *jwt.Manager, l logger.Interface) {
	// Options
	handler.Use(middleware.Logger(l))
	handler.Use(middleware.Recovery(l))

	// Prometheus metrics
	if cfg.Metrics.Enabled {
		p := ginprometheus.NewPrometheus("fileuploader-gin")
		p.Use(handler)
	}

	// Swagger
	if cfg.Swagger.Enabled {
		handler.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// K8s probe
	handler.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Routers
	apiV1Group := handler.Group("/v1")
	{
		v1.NewRoutes(apiV1Group, t, u, tk, jwtManager, l)
	}
}
