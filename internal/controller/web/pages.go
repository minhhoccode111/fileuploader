package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// _hxRequestTypeHeader carries htmx's swap kind: "partial" for a targeted swap, "full" for body swaps.
const (
	_hxRequestTypeHeader = "HX-Request-Type"
	_hxRequestPartial    = "partial"

	// _genericErrorMessage is shown when the server fails for a reason the caller
	// must not see. The real error is logged instead.
	_genericErrorMessage = "Something went wrong. Please try again."

	// _fileNotFoundMessage is shown when an entry the request names does not
	// exist or belongs to another account.
	_fileNotFoundMessage = "That file is not here any more."

	// _folderNotFoundMessage is shown when a folder the request names does not
	// exist or belongs to another account.
	_folderNotFoundMessage = "That folder is not here any more."

	// _shareNotFoundMessage is shown when a share the request names does not
	// exist, has expired or belongs to another account.
	_shareNotFoundMessage = "That share is no longer here."

	// _sessionLocal is where the auth guard parks the session it resolved, which
	// is how a handler learns who the visitor is and what role they hold.
	_sessionLocal = "session"

	// _adminPath is the admin area. It holds no page of its own: it sends the
	// visitor to the landing page of the area.
	_adminPath = "/admin"

	// _adminUsersPath is the user management page, the landing page of the
	// admin area.
	_adminUsersPath = "/admin/users"

	// _adminSettingsPath is the settings page of the admin area.
	_adminSettingsPath = "/admin/settings"

	// _adminAccessRequestsPath is the access-request approval page.
	_adminAccessRequestsPath = "/admin/access-requests"
)

// renderErrors renders the error fragment htmx swaps into a form's error target.
func (c *Controller) renderErrors(ctx *fiber.Ctx, status int, messages []string) error {
	ctx.Status(status)

	return c.renderPage(ctx, templates.ErrorList(response.Errors{Messages: messages}))
}

// renderPage renders a templ component as an HTML response.
func (c *Controller) renderPage(ctx *fiber.Ctx, component templ.Component) error {
	buf := new(bytes.Buffer)
	if err := component.Render(ctx.UserContext(), buf); err != nil {
		c.l.Error(fmt.Errorf("web - renderPage - component.Render: %w", err))

		return ctx.SendStatus(http.StatusInternalServerError)
	}

	ctx.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

	return ctx.Send(buf.Bytes())
}

// render returns the fragment for an htmx partial swap, the full page
// otherwise. A partial also carries a head-only update, so the document
// title follows the page the swap put on the screen.
func (c *Controller) render(ctx *fiber.Ctx, full, fragment templ.Component, title string) error {
	if ctx.Get(_hxRequestTypeHeader) == _hxRequestPartial {
		return c.renderPage(ctx, templ.Join(fragment, templates.TitlePartial(title)))
	}

	return c.renderPage(ctx, full)
}

func (c *Controller) login(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Login(c.view(ctx)), templates.LoginContent(), templates.LoginTitle)
}

func (c *Controller) register(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Register(c.view(ctx)), templates.RegisterContent(), templates.RegisterTitle)
}

// admin sends the visitor to the landing page of the admin area. The area is a
// namespace: every admin page lives under it, and the area itself holds no
// page of its own. The guard has already checked the role.
func (c *Controller) admin(ctx *fiber.Ctx) error {
	return middleware.Redirect(ctx, _adminUsersPath)
}

// adminUsers renders the user management page. The guard has already checked
// the role, so the handler only renders, and the address bar re-feeds the
// filter inputs it carries, which is how a bookmarked view comes back the
// way it was left. The validation runs when the table asks for its rows.
func (c *Controller) adminUsers(ctx *fiber.Ctx) error {
	req := decodeUserListQuery(ctx)
	filters := userFilters(&req)

	return c.render(ctx, templates.AdminUsers(c.view(ctx), filters), templates.AdminUsersContent(filters), templates.AdminUsersTitle)
}

// adminSettings renders the settings page. The guard has already checked the
// role, so the handler only renders.
func (c *Controller) adminSettings(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.AdminSettings(c.view(ctx)), templates.AdminSettingsContent(), templates.AdminSettingsTitle)
}

// adminAccessRequests renders the pending account requests. The guard has
// already checked the role.
func (c *Controller) adminAccessRequests(ctx *fiber.Ctx) error {
	req := decodeUserListQuery(ctx)

	filters := userFilters(&req)

	if filters.Sort == "" {
		filters.Sort = "created_at"
	}

	if filters.Dir == "" {
		filters.Dir = "desc"
	}

	if filters.Limit == "" {
		filters.Limit = "10"
	}

	if filters.Page == 0 {
		filters.Page = 1
	}

	return c.render(ctx, templates.AdminAccessRequests(c.view(ctx), filters), templates.AdminAccessRequestsContent(filters), templates.AdminAccessRequestsTitle)
}

// logout renders the confirmation screen. It is a GET because it changes nothing:
// the session is ended only by the POST to /htmx/logout that the screen triggers.
func (c *Controller) logout(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Logout(c.view(ctx)), templates.LogoutContent(), templates.LogoutTitle)
}

// view builds the per-request state the templates need. The auth guard parks
// the session it resolved, so a handler never has to look the visitor up: an
// anonymous request gets the empty view.
func (c *Controller) view(ctx *fiber.Ctx) templates.View {
	session, ok := ctx.Locals(_sessionLocal).(entity.Session)
	if !ok {
		return templates.View{}
	}

	return templates.View{
		Authenticated: true,
		Admin:         session.Role == entity.UserRoleAdmin,
	}
}
