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
	u       usecase.User
	folders usecase.Folder
	files   usecase.File
	l       logger.Interface
	v       *validator.Validate
	cfg     *config.Config
}

// NewRouter registers the transport middleware, static assets and HTML/page routes.
func NewRouter(
	app *fiber.App,
	u usecase.User,
	folders usecase.Folder,
	files usecase.File,
	l logger.Interface,
	v *validator.Validate,
	cfg *config.Config,
) {
	c := &Controller{
		u:       u,
		folders: folders,
		files:   files,
		l:       l,
		v:       v,
		cfg:     cfg,
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
	app.Get("/folders/:folder", middleware.RequireAuth(u), c.folderPage)
	app.Get("/share", c.share)
	app.Get("/public", c.public)
	app.Get("/admin", middleware.RequireAdmin(u), c.admin)
	app.Get("/admin/users", middleware.RequireAdmin(u), c.adminUsers)
	app.Get("/admin/settings", middleware.RequireAdmin(u), c.adminSettings)
	app.Get("/login", middleware.RequireGuest(u), c.login)
	app.Get("/register", middleware.RequireGuest(u), c.register)
	app.Get("/logout", middleware.RequireAuth(u), c.logout)

	// HTMX partial endpoints
	app.Post("/htmx/login", c.loginSubmit)
	app.Post("/htmx/register", c.registerSubmit)
	app.Post("/htmx/logout", c.logoutSubmit)
	app.Post("/htmx/admin/role", middleware.RequireAdmin(u), c.roleSubmit)

	// Drive
	app.Get("/htmx/folders/new", middleware.RequireAuth(u), c.folderNew)
	app.Post("/htmx/folders", middleware.RequireAuth(u), c.folderCreate)
	app.Get("/htmx/folders/:folder/edit", middleware.RequireAuth(u), c.folderEdit)
	app.Get("/htmx/folders/:folder/move", middleware.RequireAuth(u), c.folderMoveForm)
	app.Post("/htmx/folders/:folder/rename", middleware.RequireAuth(u), c.folderRename)
	app.Post("/htmx/folders/:folder/move", middleware.RequireAuth(u), c.folderMove)
	app.Post("/htmx/folders/:folder/delete", middleware.RequireAuth(u), c.folderDelete)
	app.Get("/htmx/files/new", middleware.RequireAuth(u), c.fileNew)
	app.Post("/htmx/files", middleware.RequireAuth(u), c.fileUploadSubmit)
	app.Get("/htmx/files/:file", middleware.RequireAuth(u), c.fileDrawer)
	app.Get("/htmx/files/:file/move", middleware.RequireAuth(u), c.fileMoveForm)
	app.Post("/htmx/files/:file/rename", middleware.RequireAuth(u), c.fileRename)
	app.Post("/htmx/files/:file/delete", middleware.RequireAuth(u), c.fileDelete)
	app.Post("/htmx/files/:file/move", middleware.RequireAuth(u), c.fileMove)
	app.Get("/htmx/files/:file/duplicate", middleware.RequireAuth(u), c.fileDuplicateForm)
	app.Post("/htmx/files/:file/duplicate", middleware.RequireAuth(u), c.fileDuplicate)
	app.Get("/files/:file/download", middleware.RequireAuth(u), c.fileDownload)

	app.Get("/htmx/admin/users/edit", middleware.RequireAdmin(u), c.adminUsersEdit)
	app.Post("/htmx/admin/users/edit", middleware.RequireAdmin(u), c.adminUsersEditSubmit)

	app.Get("/htmx/admin/users", middleware.RequireAdmin(u), c.adminUsersTable)
}
