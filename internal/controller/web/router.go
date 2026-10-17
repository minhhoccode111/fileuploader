package web

import (
	"net/http"

	"github.com/evrone/go-clean-template/config"
	"github.com/evrone/go-clean-template/internal/controller/web/middleware"
	"github.com/evrone/go-clean-template/internal/usecase"
	"github.com/evrone/go-clean-template/pkg/logger"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
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

	// K8s probe
	app.Get("/healthz", func(ctx *fiber.Ctx) error {
		return ctx.SendStatus(http.StatusOK)
	})

	// Static assets
	app.Static("/static", "./web/static")

	// Pages
	app.Get("/", middleware.RequireAuth(u), c.home)
	app.Get("/login", middleware.RequireGuest(u), c.login)
	app.Get("/register", middleware.RequireGuest(u), c.register)
	app.Get("/logout", middleware.RequireAuth(u), c.logout)

	// HTMX partial endpoints
	app.Post("/htmx/login", c.loginSubmit)
	app.Post("/htmx/register", c.registerSubmit)
	app.Post("/htmx/logout", c.logoutSubmit)
}
