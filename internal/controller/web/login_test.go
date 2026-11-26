package web

import (
	"context"
	"io"
	"net"
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

// fakeLoginUser answers the login form from canned values and records the
// fields the handler passed it.
type fakeLoginUser struct {
	usecase.User

	token string
	user  entity.User
	err   error

	called    bool
	email     string
	password  string
	clientKey string
}

func (f *fakeLoginUser) Login(
	_ context.Context,
	email, password, clientKey string,
) (string, entity.User, error) {
	f.called = true
	f.email = email
	f.password = password
	f.clientKey = clientKey

	return f.token, f.user, f.err
}

// loginApp wires the login form on its own, so a test sees the plain handler
// without the guards the router adds.
func loginApp(users usecase.User) *fiber.App {
	c := &Controller{
		u:   users,
		l:   logger.New("error"),
		v:   validatorx.New(),
		cfg: &config.Config{},
	}

	app := fiber.New()
	app.Post("/htmx/login", c.loginSubmit)

	return app
}

// loginRequest builds the form post the page sends.
func loginRequest(clientIP string) *http.Request {
	body := url.Values{
		"email":    {"john@example.com"},
		"password": {"password"},
	}.Encode()

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/htmx/login",
		strings.NewReader(body),
	)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	req.Header.Set(_hxRequestHeader, _hxRequestTrue)
	req.Header.Set(_realIPHeader, clientIP)

	return req
}

// loginPageApp wires the login page on its own, so a test sees the plain
// handler without the guards the router adds.
func loginPageApp(cfg *config.Config) *fiber.App {
	c := &Controller{
		l:   logger.New("error"),
		v:   validatorx.New(),
		cfg: cfg,
	}

	app := fiber.New()
	app.Get("/login", c.login)

	return app
}

// loginPageConfig returns the config a page test renders with.
func loginPageConfig(demo bool) *config.Config {
	cfg := &config.Config{}

	if demo {
		cfg.Seed.DemoEmail = "demo@example.com"
		cfg.Seed.DemoUsername = "demo"
		cfg.Seed.DemoPassword = "Demo123!"
	}

	return cfg
}

func TestLoginOffersTheDemoCredentialsWhenConfigured(t *testing.T) {
	t.Parallel()

	resp, err := loginPageApp(loginPageConfig(true)).Test(
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/login", http.NoBody),
	)
	if err != nil {
		t.Fatalf("GET /login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body error = %v, want nil", err)
	}

	if !strings.Contains(string(body), "demo@example.com") {
		t.Errorf("body does not show the demo email:\n%s", body)
	}

	if !strings.Contains(string(body), "Demo123!") {
		t.Errorf("body does not show the demo password:\n%s", body)
	}
}

func TestLoginHidesTheDemoHintWithoutConfiguration(t *testing.T) {
	t.Parallel()

	resp, err := loginPageApp(loginPageConfig(false)).Test(
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/login", http.NoBody),
	)
	if err != nil {
		t.Fatalf("GET /login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body error = %v, want nil", err)
	}

	if strings.Contains(string(body), "Fill in") {
		t.Errorf("body offers a demo account without a seeded one:\n%s", body)
	}
}

func TestLoginSubmitAnswersTooManyRequestsWhenThrottled(t *testing.T) {
	t.Parallel()

	users := &fakeLoginUser{err: entity.ErrLoginThrottled}

	resp, err := loginApp(users).Test(loginRequest("10.0.0.7"))
	if err != nil {
		t.Fatalf("POST /htmx/login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body error = %v, want nil", err)
	}

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}

	if !strings.Contains(string(body), "Too many attempts") {
		t.Errorf("body = %q, want the throttle message", body)
	}

	if got := resp.Header.Get(_hxRedirectHeader); got != "" {
		t.Errorf("HX-Redirect = %q, want it empty on a refused login", got)
	}
}

func TestLoginSubmitAnswersForbiddenWhenTheAccountIsDisabled(t *testing.T) {
	t.Parallel()

	users := &fakeLoginUser{err: entity.ErrUserDisabled}

	resp, err := loginApp(users).Test(loginRequest("10.0.0.7"))
	if err != nil {
		t.Fatalf("POST /htmx/login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body error = %v, want nil", err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	if !strings.Contains(string(body), "disabled") {
		t.Errorf("body = %q, want the disabled-account message", body)
	}
}

func TestLoginSubmitIgnoresAnInvalidProxyAddress(t *testing.T) {
	t.Parallel()

	users := &fakeLoginUser{err: entity.ErrInvalidCredentials}

	resp, err := loginApp(users).Test(loginRequest("not-an-ip"))
	if err != nil {
		t.Fatalf("POST /htmx/login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if users.clientKey == "not-an-ip" {
		t.Error("loginSubmit used the raw proxy header as the throttle key")
	}

	if net.ParseIP(users.clientKey) == nil {
		t.Errorf("client key = %q, want an IP address", users.clientKey)
	}
}

func TestLoginSubmitPassesTheProxyAddressToTheUseCase(t *testing.T) {
	t.Parallel()

	users := &fakeLoginUser{token: "session-token", user: entity.User{ID: "1"}}

	resp, err := loginApp(users).Test(loginRequest("10.0.0.7"))
	if err != nil {
		t.Fatalf("POST /htmx/login error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if !users.called {
		t.Fatal("loginSubmit did not call the use case")
	}

	if users.clientKey != "10.0.0.7" {
		t.Errorf("client key = %q, want %q", users.clientKey, "10.0.0.7")
	}

	if got := resp.Header.Get(_hxRedirectHeader); got != _homePath {
		t.Errorf("HX-Redirect = %q, want %q", got, _homePath)
	}
}
