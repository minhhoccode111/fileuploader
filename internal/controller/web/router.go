package web

import (
	"net/http"

	"github.com/evrone/go-clean-template/internal/controller/web/middleware"
	"github.com/evrone/go-clean-template/internal/usecase"
	"github.com/evrone/go-clean-template/pkg/logger"
	"github.com/gofiber/fiber/v2"
)

// Controller renders HTML pages.
type Controller struct {
	u usecase.User
	l logger.Interface
}

// NewRouter registers the transport middleware, static assets and HTML/page routes.
func NewRouter(app *fiber.App, u usecase.User, l logger.Interface) {
	c := &Controller{u: u, l: l}

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
	app.Get("/", c.home)
	app.Get("/login", c.login)
	app.Get("/register", c.register)
	app.Get("/logout", c.logout)
}
