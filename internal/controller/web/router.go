package web

import (
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// Controller renders HTML pages and handles page form submissions.
type Controller struct {
	u   usecase.User
	l   logger.Interface
	v   *validator.Validate
	cfg *config.Config
}

// NewRouter registers the transport middleware, static assets and HTML/page routes.
func NewRouter(
	app *fiber.App,
	u usecase.User,
	l logger.Interface,
	v *validator.Validate,
	cfg *config.Config,
) {
	c := &Controller{
		u:   u,
		l:   l,
		v:   v,
		cfg: cfg,
	}

	// Options
	app.Use(middleware.Logger(l))
	app.Use(middleware.Recovery(l))
	app.Use(middleware.Latency(cfg.HTTP.FakeLatencyMin, cfg.HTTP.FakeLatencyMax))

	// K8s probe
	app.Get("/healthz", func(ctx *fiber.Ctx) error {
		return ctx.SendStatus(http.StatusOK)
	})

	// Static assets
	app.Static("/static", "./web/static")

	// Pages
	app.Get("/", middleware.RequireAuth(u), c.home)
	app.Get("/share", middleware.RequireAuth(u), c.share)
	app.Get("/public", middleware.RequireAuth(u), c.public)
	app.Get("/admin", middleware.RequireAdmin(u), c.admin)
	app.Get(_adminUsersPath, middleware.RequireAdmin(u), c.adminUsers)
	app.Get(_adminSettingsPath, middleware.RequireAdmin(u), c.adminSettings)
	app.Get("/login", middleware.RequireGuest(u), c.login)
	app.Get("/register", middleware.RequireGuest(u), c.register)
	app.Get("/logout", middleware.RequireAuth(u), c.logout)

	// HTMX partial endpoints
	app.Post("/htmx/login", c.loginSubmit)
	app.Post("/htmx/register", c.registerSubmit)
	app.Post("/htmx/logout", c.logoutSubmit)
	app.Post("/htmx/admin/role", middleware.RequireAdmin(u), c.roleSubmit)

	app.Get(_adminUsersTablePath, middleware.RequireAdmin(u), c.adminUsersTable)
}
