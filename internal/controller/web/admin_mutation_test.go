package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

const (
	_adminTestUserID = "550e8400-e29b-41d4-a716-446655440000"
	_adminTestAdmin  = "admin-1"
)

// errAdminList is the failure the fake user repository reports for a list.
var errAdminList = errors.New("database down")

type fakeAdminUser struct {
	usecase.User

	user       entity.User
	list       entity.UserList
	listErr    error
	updateErr  error
	limitsErr  error
	statusErr  error
	roleErr    error
	approveErr error

	lastFilter    entity.UserFilter
	lastUserID    string
	lastUsername  string
	lastEmail     string
	lastRole      entity.UserRole
	lastPassword  string
	lastStatus    entity.UserStatus
	lastQuota     int64
	lastUpload    int64
	lastDownload  int64
	lastApproved  string
	listCalls     int
	getCalls      int
	updateCalls   int
	limitsCalls   int
	statusCalls   int
	roleCalls     int
	approveCalls  int
	sessionsEnded bool
}

func (f *fakeAdminUser) ListUsers(_ context.Context, filter *entity.UserFilter) (entity.UserList, error) {
	f.listCalls++
	f.lastFilter = *filter

	if f.listErr != nil {
		return entity.UserList{}, f.listErr
	}

	return f.list, nil
}

func (f *fakeAdminUser) GetUser(_ context.Context, userID string) (entity.User, error) {
	f.getCalls++

	if f.user.ID == "" {
		return entity.User{}, entity.ErrUserNotFound
	}

	f.lastUserID = userID

	return f.user, nil
}

func (f *fakeAdminUser) UpdateUser(
	_ context.Context,
	userID, username, email string,
	role entity.UserRole,
	password string,
) (entity.User, bool, error) {
	f.updateCalls++
	f.lastUserID = userID
	f.lastUsername = username
	f.lastEmail = email
	f.lastRole = role
	f.lastPassword = password

	if f.updateErr != nil {
		return entity.User{}, false, f.updateErr
	}

	f.user.ID = userID
	f.user.Username = username
	f.user.Email = email
	f.user.Role = role

	return f.user, f.sessionsEnded, nil
}

func (f *fakeAdminUser) SetUserLimits(
	_ context.Context,
	userID string,
	quota, uploadRate, downloadRate int64,
) (entity.User, error) {
	f.limitsCalls++
	f.lastUserID = userID
	f.lastQuota = quota
	f.lastUpload = uploadRate
	f.lastDownload = downloadRate

	if f.limitsErr != nil {
		return entity.User{}, f.limitsErr
	}

	f.user.StorageQuotaBytes = quota
	f.user.UploadRateBytesPerSec = uploadRate
	f.user.DownloadRateBytesPerSec = downloadRate

	return f.user, nil
}

func (f *fakeAdminUser) SetStatus(
	_ context.Context,
	userID string,
	status entity.UserStatus,
) error {
	f.statusCalls++
	f.lastUserID = userID
	f.lastStatus = status

	return f.statusErr
}

func (f *fakeAdminUser) SetRole(_ context.Context, email string, role entity.UserRole) error {
	f.roleCalls++
	f.lastEmail = email
	f.lastRole = role

	return f.roleErr
}

func (f *fakeAdminUser) ApproveUser(_ context.Context, userID string) error {
	f.approveCalls++
	f.lastApproved = userID

	return f.approveErr
}

// adminMutationApp wires the admin handlers behind a session middleware. The
// config carries the environment defaults the drawer and the rows word an
// inheriting account against.
func adminMutationApp(users usecase.User, cfg *config.Config) *fiber.App {
	c := &Controller{
		u:   users,
		l:   logger.New("error"),
		v:   validatorx.New(),
		cfg: cfg,
	}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _adminTestAdmin, Role: entity.UserRoleAdmin})

		return ctx.Next()
	})

	app.Get("/htmx/admin/users", c.adminUsersTable)
	app.Get("/htmx/admin/users/edit", c.adminUsersEdit)
	app.Post("/htmx/admin/users/edit", c.adminUsersEditSubmit)
	app.Post("/htmx/admin/role", c.roleSubmit)
	app.Get("/htmx/admin/access-requests", c.adminAccessRequestsTable)
	app.Post("/htmx/admin/access-requests/approve", c.adminAccessRequestApprove)

	return app
}

func adminForm(
	t *testing.T,
	app *fiber.App,
	path string,
	form url.Values,
) (status int, head http.Header, body string) {
	t.Helper()

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		path,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, res.Header, string(raw)
}

func TestAdminUsersTablePassesTheFilter(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{list: entity.UserList{
		Items: []entity.UserListItem{{ID: _adminTestUserID, Username: "john"}},
		Total: 1,
	}}

	status, _, body := adminResponse(
		t,
		adminMutationApp(users, &config.Config{}),
		"/htmx/admin/users?q=john&role=admin&sort=email&dir=asc&limit=10&page=2",
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.listCalls != 1 {
		t.Fatalf("ListUsers calls = %d, want 1", users.listCalls)
	}

	want := entity.UserFilter{
		Query: "john",
		Role:  entity.UserRoleAdmin,
		Sort:  entity.UserSortEmail,
		Dir:   entity.UserSortAscending,
		Limit: 10,
		Page:  2,
	}
	if users.lastFilter != want {
		t.Errorf("filter = %+v, want %+v", users.lastFilter, want)
	}

	if !strings.Contains(body, "john") {
		t.Errorf("body = %q, want the account row", body)
	}
}

func TestAdminUsersTableRejectsAMalformedQuery(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, _, _ := adminResponse(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users?sort=password_hash", nil)

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if users.listCalls != 0 {
		t.Errorf("ListUsers calls = %d, want 0", users.listCalls)
	}
}

func TestAdminUsersTablePushesAChangedURL(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, headers, _ := adminResponse(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users?q=john", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	pushed := headers.Get(_hxPushURLHeader)
	if !strings.Contains(pushed, "/admin/users?") || !strings.Contains(pushed, "q=john") {
		t.Errorf("HX-Push-Url = %q, want the filtered page path", pushed)
	}
}

func TestAdminUsersTableSkipsThePushWhenTheURLIsUnchanged(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, headers, _ := adminResponse(
		t,
		adminMutationApp(users, &config.Config{}),
		"/htmx/admin/users?q=john",
		map[string]string{_hxCurrentURLHeader: "http://x/admin/users?q=john"},
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if pushed := headers.Get(_hxPushURLHeader); pushed != "" {
		t.Errorf("HX-Push-Url = %q, want no push", pushed)
	}
}

func TestAdminUsersTableReportsAListFailure(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{listErr: errAdminList}

	status, _, _ := adminResponse(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users", nil)

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
}

func TestAdminUsersEditRendersTheAccount(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{user: entity.User{
		ID:       _adminTestUserID,
		Username: "john",
		Email:    "john@example.com",
		Role:     entity.UserRoleUser,
		Status:   entity.UserStatusActive,
	}}

	status, _, body := adminResponse(
		t,
		adminMutationApp(users, &config.Config{}),
		"/htmx/admin/users/edit?id="+_adminTestUserID,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.getCalls != 1 {
		t.Errorf("GetUser calls = %d, want 1", users.getCalls)
	}

	for _, part := range []string{"john", "john@example.com"} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// The drawer names the default an empty limit field inherits, so the admin
// sees the limit the account actually runs at instead of a bare zero.
func TestAdminUsersEditWordsTheInheritedDefaults(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{user: entity.User{
		ID:       _adminTestUserID,
		Username: "john",
		Email:    "john@example.com",
		Role:     entity.UserRoleUser,
		Status:   entity.UserStatusActive,
	}}

	cfg := &config.Config{}
	cfg.Limits.StorageQuotaBytes = 5368709120
	cfg.Limits.UploadRateBytesPerSec = 1048576

	status, _, body := adminResponse(
		t,
		adminMutationApp(users, cfg),
		"/htmx/admin/users/edit?id="+_adminTestUserID,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	for _, part := range []string{
		"Zero inherits the default. 5.0 GB",
		"Zero inherits the default. 1.0 MB/s",
	} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

func TestAdminUsersEditRejectsABadID(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, _, _ := adminResponse(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit?id=not-a-uuid", nil)

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if users.getCalls != 0 {
		t.Errorf("GetUser calls = %d, want 0", users.getCalls)
	}
}

func TestAdminUsersEditRejectsAnUnknownAccount(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, _, _ := adminResponse(
		t,
		adminMutationApp(users, &config.Config{}),
		"/htmx/admin/users/edit?id="+_adminTestUserID,
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

//nolint:cyclop,gocyclo // one assertion per changed field of the account.
func TestAdminUsersEditSubmitUpdatesTheAccount(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{user: entity.User{
		ID:       _adminTestUserID,
		Username: "john",
		Email:    "john@example.com",
		Role:     entity.UserRoleUser,
		Status:   entity.UserStatusActive,
	}}

	status, headers, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
		"id":            {_adminTestUserID},
		"username":      {"johnny"},
		"email":         {"johnny@example.com"},
		"role":          {"admin"},
		"status":        {"active"},
		"storage_quota": {"1K"},
		"upload_rate":   {"2K"},
		"download_rate": {"3K"},
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.updateCalls != 1 || users.limitsCalls != 1 {
		t.Errorf("UpdateUser calls = %d, SetUserLimits calls = %d, want 1 and 1", users.updateCalls, users.limitsCalls)
	}

	if users.lastUserID != _adminTestUserID || users.lastUsername != "johnny" ||
		users.lastEmail != "johnny@example.com" || users.lastRole != entity.UserRoleAdmin {
		t.Errorf(
			"updated account = %q/%q/%q/%q, want id/johnny/johnny@example.com/admin",
			users.lastUserID,
			users.lastUsername,
			users.lastEmail,
			users.lastRole,
		)
	}

	if users.lastQuota != 1024 || users.lastUpload != 2048 || users.lastDownload != 3072 {
		t.Errorf("limits = %d/%d/%d, want 1024/2048/3072", users.lastQuota, users.lastUpload, users.lastDownload)
	}

	if users.statusCalls != 0 {
		t.Errorf("SetStatus calls = %d, want 0 for an active status", users.statusCalls)
	}

	if got := headers.Get(_hxTriggerHeader); got != _usersUpdatedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _usersUpdatedEvent)
	}
}

func TestAdminUsersEditSubmitRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		quota string
	}{
		{name: "an unknown suffix", quota: "1XB"},
		{name: "a negative value", quota: "-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			users := &fakeAdminUser{user: entity.User{ID: _adminTestUserID, Status: entity.UserStatusActive}}

			status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
				"id":            {_adminTestUserID},
				"username":      {"johnny"},
				"email":         {"johnny@example.com"},
				"role":          {"user"},
				"storage_quota": {tc.quota},
			})

			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
			}

			if users.updateCalls != 0 {
				t.Errorf("UpdateUser calls = %d, want 0", users.updateCalls)
			}
		})
	}
}

func TestAdminUsersEditSubmitEndsSessionsForANonActiveStatus(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{user: entity.User{
		ID:       _adminTestUserID,
		Username: "john",
		Email:    "john@example.com",
		Role:     entity.UserRoleUser,
		Status:   entity.UserStatusActive,
	}}

	status, headers, body := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
		"id":       {_adminTestUserID},
		"username": {"john"},
		"email":    {"john@example.com"},
		"role":     {"user"},
		"status":   {"disabled"},
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.statusCalls != 1 || users.lastStatus != entity.UserStatusDisabled {
		t.Errorf("SetStatus = %d/%q, want 1/disabled", users.statusCalls, users.lastStatus)
	}

	if !strings.Contains(body, "Every live session of the account was ended") {
		t.Errorf("body = %q, want the session-revocation note", body)
	}

	if got := headers.Get(_hxTriggerHeader); got != _usersUpdatedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _usersUpdatedEvent)
	}
}

func TestAdminUsersEditSubmitReportsAConflict(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{
		user:      entity.User{ID: _adminTestUserID, Status: entity.UserStatusActive},
		updateErr: entity.ErrEmailAlreadyExists,
	}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
		"id":       {_adminTestUserID},
		"username": {"johnny"},
		"email":    {"johnny@example.com"},
		"role":     {"user"},
	})

	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}
}

func TestAdminUsersEditSubmitReportsALimitsFailure(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{
		user:      entity.User{ID: _adminTestUserID, Status: entity.UserStatusActive},
		limitsErr: entity.ErrInvalidLimits,
	}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
		"id":            {_adminTestUserID},
		"username":      {"johnny"},
		"email":         {"johnny@example.com"},
		"role":          {"user"},
		"storage_quota": {"1K"},
	})

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if users.statusCalls != 0 {
		t.Errorf("SetStatus calls = %d, want 0 after a failed limits write", users.statusCalls)
	}
}

func TestAdminUsersEditSubmitReportsAStatusFailure(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{
		user:      entity.User{ID: _adminTestUserID, Status: entity.UserStatusActive},
		statusErr: entity.ErrUserNotFound,
	}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/users/edit", url.Values{
		"id":       {_adminTestUserID},
		"username": {"johnny"},
		"email":    {"johnny@example.com"},
		"role":     {"user"},
		"status":   {"disabled"},
	})

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestRoleSubmitUpdatesTheRole(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, headers, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/role", url.Values{
		"email": {"John@Example.COM"},
		"role":  {"Admin"},
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.roleCalls != 1 {
		t.Fatalf("SetRole calls = %d, want 1", users.roleCalls)
	}

	if users.lastEmail != "john@example.com" || users.lastRole != entity.UserRoleAdmin {
		t.Errorf("SetRole = %q/%q, want john@example.com/admin", users.lastEmail, users.lastRole)
	}

	if got := headers.Get(_hxTriggerHeader); got != _usersUpdatedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _usersUpdatedEvent)
	}
}

func TestRoleSubmitRejectsAnUnknownAccount(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{roleErr: entity.ErrUserNotFound}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/role", url.Values{
		"email": {"missing@example.com"},
		"role":  {"admin"},
	})

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestRoleSubmitRejectsAnInvalidRole(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/role", url.Values{
		"email": {"john@example.com"},
		"role":  {"root"},
	})

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if users.roleCalls != 0 {
		t.Errorf("SetRole calls = %d, want 0", users.roleCalls)
	}
}

func TestAccessRequestsTableForcesPending(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{list: entity.UserList{Items: []entity.UserListItem{{ID: _adminTestUserID, Username: "pending"}}}}

	status, _, body := adminResponse(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/access-requests?q=pending", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if users.lastFilter.Status != entity.UserStatusPending {
		t.Errorf("status filter = %q, want pending", users.lastFilter.Status)
	}

	if users.lastFilter.Sort != entity.UserSortCreatedAt || users.lastFilter.Dir != entity.UserSortDescending {
		t.Errorf("order = %q/%q, want created_at/desc", users.lastFilter.Sort, users.lastFilter.Dir)
	}

	if !strings.Contains(body, "pending") {
		t.Errorf("body = %q, want the pending account", body)
	}
}

func TestAccessRequestApproveReturnsNoContent(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, headers, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/access-requests/approve", url.Values{
		"id": {_adminTestUserID},
	})

	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", status, http.StatusNoContent)
	}

	if users.approveCalls != 1 || users.lastApproved != _adminTestUserID {
		t.Errorf("ApproveUser calls = %d, id = %q, want 1 and %q", users.approveCalls, users.lastApproved, _adminTestUserID)
	}

	if got := headers.Get(_hxTriggerHeader); got != "accessRequestsUpdated" {
		t.Errorf("HX-Trigger = %q, want accessRequestsUpdated", got)
	}
}

func TestAccessRequestApproveRejectsABadID(t *testing.T) {
	t.Parallel()

	users := &fakeAdminUser{}

	status, _, _ := adminForm(t, adminMutationApp(users, &config.Config{}), "/htmx/admin/access-requests/approve", url.Values{})

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
	}

	if users.approveCalls != 0 {
		t.Errorf("ApproveUser calls = %d, want 0", users.approveCalls)
	}
}
