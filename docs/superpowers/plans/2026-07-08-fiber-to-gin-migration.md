# Fiber to Gin Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all Fiber HTTP framework references with Gin across the entire fileuploader project.

**Architecture:** Swap `pkg/httpserver` from wrapping `*fiber.App` to wrapping `*gin.Engine` + `*http.Server`. All middleware and controller handlers are rewritten from `*fiber.Ctx` signatures to `gin.HandlerFunc`. The Gin patterns follow the proven eform project migration exactly — same dependency choices, same middleware patterns, same handler signatures.

**Tech Stack:** Go 1.26, `github.com/gin-gonic/gin` (replaces Fiber), `github.com/swaggo/gin-swagger` (replaces gofiber/swagger), `github.com/zsais/go-gin-prometheus` (replaces fiberprometheus).

---

### Task 1: Update `go.mod` — swap Fiber deps for Gin deps

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Remove Fiber dependencies and add Gin dependencies**

Replace the three Fiber direct dependencies with their Gin equivalents:

```
Remove three lines:
    github.com/ansrivas/fiberprometheus/v2 v2.17.0
    github.com/gofiber/fiber/v2 v2.52.13
    github.com/gofiber/swagger v1.1.1

Add these lines (in alphabetic order in the require block):
    github.com/gin-gonic/gin v1.12.0
    github.com/swaggo/files v1.0.1
    github.com/swaggo/gin-swagger v1.6.1
    github.com/zsais/go-gin-prometheus v1.0.3
```

- [ ] **Step 2: Run go mod tidy to resolve all transitive deps**

Run: `go mod tidy`
Expected: exits cleanly, go.sum is updated, no more `gofiber/` entries.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): replace fiber with gin dependencies"
```

---

### Task 2: Rewrite `pkg/httpserver` — swap Fiber for Gin

**Files:**
- Modify: `pkg/httpserver/server.go`
- Modify: `pkg/httpserver/options.go`

- [ ] **Step 1: Rewrite server.go**

Replace the entire file content:

```go
// Package httpserver implements HTTP server.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"golang.org/x/sync/errgroup"
)

const (
	_defaultAddr            = ":80"
	_defaultReadTimeout     = 5 * time.Second
	_defaultWriteTimeout    = 5 * time.Second
	_defaultShutdownTimeout = 3 * time.Second
)

// Server -.
type Server struct {
	ctx context.Context
	eg  *errgroup.Group

	Engine *gin.Engine
	server *http.Server
	notify chan error

	address         string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	shutdownTimeout time.Duration

	logger logger.Interface
}

// New -.
func New(l logger.Interface, opts ...Option) *Server {
	group, ctx := errgroup.WithContext(context.Background())
	group.SetLimit(1) // Run only one goroutine

	s := &Server{
		ctx:             ctx,
		eg:              group,
		Engine:          nil,
		notify:          make(chan error, 1),
		address:         _defaultAddr,
		readTimeout:     _defaultReadTimeout,
		writeTimeout:    _defaultWriteTimeout,
		shutdownTimeout: _defaultShutdownTimeout,
		logger:          l,
	}

	// Custom options
	for _, opt := range opts {
		opt(s)
	}

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()

	s.Engine = engine

	return s
}

// Start -.
func (s *Server) Start() {
	s.server = &http.Server{
		Addr:         s.address,
		Handler:      s.Engine,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
	}

	s.eg.Go(func() error {
		err := s.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.notify <- err

			close(s.notify)

			return err
		}

		return nil
	})

	s.logger.Info("restapi server - Server - Started")
}

// Notify -.
func (s *Server) Notify() <-chan error {
	return s.notify
}

// Shutdown -.
func (s *Server) Shutdown() error {
	var shutdownErrors []error

	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	err := s.server.Shutdown(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error(err, "restapi server - Server - Shutdown - s.server.Shutdown")

		shutdownErrors = append(shutdownErrors, err)
	}

	// Wait for all goroutines to finish and get any error
	err = s.eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error(err, "restapi server - Server - Shutdown - s.eg.Wait")

		shutdownErrors = append(shutdownErrors, err)
	}

	s.logger.Info("restapi server - Server - Shutdown")

	return errors.Join(shutdownErrors...)
}
```

- [ ] **Step 2: Rewrite options.go — remove Prefork**

```go
package httpserver

import (
	"net"
	"time"
)

// Option -.
type Option func(*Server)

// Port -.
func Port(port string) Option {
	return func(s *Server) {
		s.address = net.JoinHostPort("", port)
	}
}

// ReadTimeout -.
func ReadTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		s.readTimeout = timeout
	}
}

// WriteTimeout -.
func WriteTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		s.writeTimeout = timeout
	}
}

// ShutdownTimeout -.
func ShutdownTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		s.shutdownTimeout = timeout
	}
}
```

- [ ] **Step 3: Commit**

```bash
git add pkg/httpserver/server.go pkg/httpserver/options.go
git commit -m "refactor(pkg/httpserver): replace fiber with gin"
```

---

### Task 3: Update `internal/app/app.go` wiring

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Change field name from App to Engine**

Find: `httpServer.App,`
Replace with: `httpServer.Engine,`

Also remove the `Prefix` option from the httpserver.New call:
Find: `httpserver.New(l, httpserver.Port(cfg.HTTP.Port), httpserver.Prefork(cfg.HTTP.UsePreforkMode))`
Replace with: `httpserver.New(l, httpserver.Port(cfg.HTTP.Port))`

- [ ] **Step 2: Commit**

```bash
git add internal/app/app.go
git commit -m "refactor(app): use httpServer.Engine instead of httpServer.App"
```

---

### Task 4: Rewrite middleware files

**Files:**
- Modify: `internal/controller/restapi/middleware/logger.go`
- Modify: `internal/controller/restapi/middleware/recovery.go`
- Modify: `internal/controller/restapi/middleware/auth.go`
- Modify: `internal/controller/restapi/middleware/auth_test.go`

- [ ] **Step 1: Rewrite logger.go**

```go
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

func Logger(l logger.Interface) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		l.Info("%s - %s %s - %d %d",
			c.ClientIP(),
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			c.Writer.Size(),
		)
	}
}
```

- [ ] **Step 2: Rewrite recovery.go**

```go
package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/response"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

func Recovery(l logger.Interface) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				l.Error("%s - %s %s PANIC DETECTED: %v\n%s\n",
					c.ClientIP(),
					c.Request.Method,
					c.Request.URL.Path,
					err,
					debug.Stack(),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, response.Error{
					Error: "Internal Server Error",
				})
			}
		}()

		c.Next()
	}
}
```

- [ ] **Step 3: Rewrite auth.go**

```go
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/pkg/jwt"
)

const _bearerParts = 2

type errorResponse struct {
	Error string `json:"error"`
}

// Auth returns a JWT authentication middleware for Gin.
func Auth(jwtManager *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "missing authorization header"})

			return
		}

		parts := strings.SplitN(header, " ", _bearerParts)
		if len(parts) != _bearerParts || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "invalid authorization header format"})

			return
		}

		userID, err := jwtManager.ParseToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "invalid or expired token"})

			return
		}

		c.Set("userID", userID)

		c.Next()
	}
}
```

- [ ] **Step 4: Rewrite auth_test.go**

```go
package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/middleware"
	"github.com/minhhoccode111/fileuploader/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestApp(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()

	jwtManager := jwt.New("test-secret", time.Hour)

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middleware.Auth(jwtManager))
	engine.GET("/test", func(c *gin.Context) {
		userID, ok := c.Get("userID")
		if !ok {
			c.Status(http.StatusUnauthorized)

			return
		}

		c.String(http.StatusOK, userID.(string))
	})

	return engine, jwtManager
}

func TestAuthMiddleware(t *testing.T) {
	t.Parallel()

	app, jwtManager := newTestApp(t)

	validToken, err := jwtManager.GenerateToken("user-id-123")
	require.NoError(t, err)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "missing header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid format",
			authHeader:     "Basic xxx",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid token",
			authHeader:     "Bearer invalid",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "valid token",
			authHeader:     "Bearer " + validToken,
			expectedStatus: http.StatusOK,
			expectedBody:   "user-id-123",
		},
	}

	for _, tc := range tests {
		localTc := tc

		t.Run(localTc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(app)
			defer server.Close()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/test", http.NoBody)
			require.NoError(t, err)

			if localTc.authHeader != "" {
				req.Header.Set("Authorization", localTc.authHeader)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, localTc.expectedStatus, resp.StatusCode)

			if localTc.expectedBody != "" {
				body, readErr := io.ReadAll(resp.Body)
				require.NoError(t, readErr)
				assert.Equal(t, localTc.expectedBody, string(body))
			}
		})
	}
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/controller/restapi/middleware/
git commit -m "refactor(middleware): replace fiber handlers with gin.HandlerFunc"
```

---

### Task 5: Rewrite `internal/controller/restapi/router.go`

**Files:**
- Modify: `internal/controller/restapi/router.go`

- [ ] **Step 1: Replace entire file content**

```go
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
```

- [ ] **Step 2: Commit**

```bash
git add internal/controller/restapi/router.go
git commit -m "refactor(router): replace fiber routing with gin"
```

---

### Task 6: Rewrite v1 error.go and router.go

**Files:**
- Modify: `internal/controller/restapi/v1/error.go`
- Modify: `internal/controller/restapi/v1/router.go`

- [ ] **Step 1: Rewrite error.go to use gin pattern**

```go
package v1

import (
	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/response"
)

func errorResponse(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, response.Error{Error: msg})
}
```

- [ ] **Step 2: Rewrite router.go to use gin routing**

```go
package v1

import (
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/middleware"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/jwt"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// NewRoutes -.
func NewRoutes(apiV1Group *gin.RouterGroup, t usecase.Translation, u usecase.User, tk usecase.Task, jwtManager *jwt.Manager, l logger.Interface) {
	r := &V1{t: t, u: u, tk: tk, l: l, v: validator.New(validator.WithRequiredStructEnabled())}

	// Public routes
	authGroup := apiV1Group.Group("/auth")
	{
		authGroup.POST("/register", r.register)
		authGroup.POST("/login", r.login)
	}

	// Protected routes
	protected := apiV1Group.Group("", middleware.Auth(jwtManager))

	userGroup := protected.Group("/user")
	{
		userGroup.GET("/profile", r.profile)
	}

	taskGroup := protected.Group("/tasks")
	{
		taskGroup.POST("/", r.createTask)
		taskGroup.GET("/", r.listTasks)
		taskGroup.GET("/:id", r.getTask)
		taskGroup.PUT("/:id", r.updateTask)
		taskGroup.PATCH("/:id/status", r.transitionTask)
		taskGroup.DELETE("/:id", r.deleteTask)
	}

	translationGroup := protected.Group("/translation")
	{
		translationGroup.GET("/history", r.history)
		translationGroup.POST("/do-translate", r.doTranslate)
	}
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/controller/restapi/v1/error.go internal/controller/restapi/v1/router.go
git commit -m "refactor(v1): replace fiber router with gin router"
```

---

### Task 7: Rewrite `v1/user.go` — swap Fiber ctx for Gin ctx

**Files:**
- Modify: `internal/controller/restapi/v1/user.go`

- [ ] **Step 1: Replace entire file content**

```go
package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// @Summary     Register
// @Description Register a new user
// @ID          register
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body     request.Register true "Registration data"
// @Success     201     {object} entity.User
// @Failure     400     {object} response.Error
// @Failure     409     {object} response.Error
// @Failure     500     {object} response.Error
// @Router      /auth/register [post]
func (r *V1) register(c *gin.Context) {
	var body request.Register

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - register")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - register")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	user, err := r.u.Register(c.Request.Context(), body.Username, body.Email, body.Password)
	if err != nil {
		r.l.Error(err, "restapi - v1 - register")

		if errors.Is(err, entity.ErrUserAlreadyExists) {
			errorResponse(c, http.StatusConflict, "user already exists")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusCreated, user)
}

// @Summary     Login
// @Description Authenticate user and get JWT token
// @ID          login
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body     request.Login true "Login credentials"
// @Success     200     {object} response.Token
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     500     {object} response.Error
// @Router      /auth/login [post]
func (r *V1) login(c *gin.Context) {
	var body request.Login

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - login")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - login")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	token, err := r.u.Login(c.Request.Context(), body.Email, body.Password)
	if err != nil {
		r.l.Error(err, "restapi - v1 - login")

		if errors.Is(err, entity.ErrInvalidCredentials) {
			errorResponse(c, http.StatusUnauthorized, "invalid credentials")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, response.Token{Token: token})
}

// @Summary     Get profile
// @Description Get current user profile
// @ID          profile
// @Tags        user
// @Produce     json
// @Success     200 {object} entity.User
// @Failure     401 {object} response.Error
// @Failure     404 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /user/profile [get]
func (r *V1) profile(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	user, err := r.u.GetUser(c.Request.Context(), id)
	if err != nil {
		r.l.Error(err, "restapi - v1 - profile")

		if errors.Is(err, entity.ErrUserNotFound) {
			errorResponse(c, http.StatusNotFound, "user not found")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, user)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/controller/restapi/v1/user.go
git commit -m "refactor(v1/user): replace fiber ctx with gin ctx"
```

---

### Task 8: Rewrite `v1/task.go` — swap Fiber ctx for Gin ctx

**Files:**
- Modify: `internal/controller/restapi/v1/task.go`

- [ ] **Step 1: Replace entire file content**

```go
package v1

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// @Summary     Create task
// @Description Create a new task for the current user
// @ID          create-task
// @Tags        tasks
// @Accept      json
// @Produce     json
// @Param       request body     request.CreateTask true "Task data"
// @Success     201     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Router      /tasks [post]
func (r *V1) createTask(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	var body request.CreateTask

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	task, err := r.tk.Create(c.Request.Context(), id, body.Title, body.Description)
	if err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusCreated, task)
}

// @Summary     List tasks
// @Description List tasks for the current user with optional filtering
// @ID          list-tasks
// @Tags        tasks
// @Produce     json
// @Param       status query    string false "Filter by status" Enums(todo, in_progress, done)
// @Param       limit  query    int    false "Limit"  default(10)
// @Param       offset query    int    false "Offset" default(0)
// @Success     200    {object} response.TaskList
// @Failure     401    {object} response.Error
// @Failure     500    {object} response.Error
// @Security    BearerAuth
// @Router      /tasks [get]
func (r *V1) listTasks(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	var status *entity.TaskStatus

	if s := c.Query("status"); s != "" {
		ts := entity.TaskStatus(s)
		if !ts.Valid() {
			errorResponse(c, http.StatusBadRequest, "invalid task status")

			return
		}

		status = &ts
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil {
		limit = 10
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		offset = 0
	}

	tasks, total, err := r.tk.List(c.Request.Context(), id, status, limit, offset)
	if err != nil {
		r.l.Error(err, "restapi - v1 - listTasks")

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, response.TaskList{
		Tasks: tasks,
		Total: total,
	})
}

// @Summary     Get task
// @Description Get a task by ID
// @ID          get-task
// @Tags        tasks
// @Produce     json
// @Param       id  path     string true "Task ID"
// @Success     200 {object} entity.Task
// @Failure     401 {object} response.Error
// @Failure     403 {object} response.Error
// @Failure     404 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id} [get]
func (r *V1) getTask(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	taskID := c.Param("id")

	task, err := r.tk.Get(c.Request.Context(), id, taskID)
	if err != nil {
		r.l.Error(err, "restapi - v1 - getTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			errorResponse(c, http.StatusNotFound, "task not found")

			return
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			errorResponse(c, http.StatusForbidden, "forbidden")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, task)
}

// @Summary     Update task
// @Description Update task title and description
// @ID          update-task
// @Tags        tasks
// @Accept      json
// @Produce     json
// @Param       id      path     string            true "Task ID"
// @Param       request body     request.UpdateTask  true "Updated task data"
// @Success     200     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     403     {object} response.Error
// @Failure     404     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id} [put]
func (r *V1) updateTask(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	taskID := c.Param("id")

	var body request.UpdateTask

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	task, err := r.tk.Update(c.Request.Context(), id, taskID, body.Title, body.Description)
	if err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			errorResponse(c, http.StatusNotFound, "task not found")

			return
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			errorResponse(c, http.StatusForbidden, "forbidden")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, task)
}

// @Summary     Transition task status
// @Description Change task status (todo -> in_progress -> done, or in_progress -> todo)
// @ID          transition-task
// @Tags        tasks
// @Accept      json
// @Produce     json
// @Param       id      path     string                true "Task ID"
// @Param       request body     request.TransitionTask  true "New status"
// @Success     200     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     403     {object} response.Error
// @Failure     404     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id}/status [patch]
func (r *V1) transitionTask(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	taskID := c.Param("id")

	var body request.TransitionTask

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	task, err := r.tk.Transition(c.Request.Context(), id, taskID, body.Status)
	if err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			errorResponse(c, http.StatusNotFound, "task not found")

			return
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			errorResponse(c, http.StatusForbidden, "forbidden")

			return
		}

		if errors.Is(err, entity.ErrInvalidTransition) {
			errorResponse(c, http.StatusBadRequest, "invalid status transition")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusOK, task)
}

// @Summary     Delete task
// @Description Delete a task by ID
// @ID          delete-task
// @Tags        tasks
// @Param       id  path     string true "Task ID"
// @Success     204 "No Content"
// @Failure     401 {object} response.Error
// @Failure     404 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id} [delete]
func (r *V1) deleteTask(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	taskID := c.Param("id")

	err := r.tk.Delete(c.Request.Context(), id, taskID)
	if err != nil {
		r.l.Error(err, "restapi - v1 - deleteTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			errorResponse(c, http.StatusNotFound, "task not found")

			return
		}

		errorResponse(c, http.StatusInternalServerError, "internal server error")

		return
	}

	c.JSON(http.StatusNoContent, nil)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/controller/restapi/v1/task.go
git commit -m "refactor(v1/task): replace fiber ctx with gin ctx"
```

---

### Task 9: Rewrite `v1/translation.go` — swap Fiber ctx for Gin ctx

**Files:**
- Modify: `internal/controller/restapi/v1/translation.go`

- [ ] **Step 1: Replace entire file content**

```go
package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/request"
	_ "github.com/minhhoccode111/fileuploader/internal/controller/restapi/v1/response" // for swaggo
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// @Summary     Show history
// @Description Show all translation history for current user
// @ID          history
// @Tags        translation
// @Produce     json
// @Success     200 {object} entity.TranslationHistory
// @Failure     401 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /translation/history [get]
func (r *V1) history(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	translationHistory, err := r.t.History(c.Request.Context(), id)
	if err != nil {
		r.l.Error(err, "restapi - v1 - history")

		errorResponse(c, http.StatusInternalServerError, "database problems")

		return
	}

	c.JSON(http.StatusOK, translationHistory)
}

// @Summary     Translate
// @Description Translate a text
// @ID          do-translate
// @Tags        translation
// @Accept      json
// @Produce     json
// @Param       request body     request.Translate true "Set up translation"
// @Success     200     {object} entity.Translation
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Router      /translation/do-translate [post]
func (r *V1) doTranslate(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	id, ok := userID.(string)
	if !ok {
		errorResponse(c, http.StatusUnauthorized, "unauthorized")

		return
	}

	var body request.Translate

	if err := c.ShouldBindJSON(&body); err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		errorResponse(c, http.StatusBadRequest, "invalid request body")

		return
	}

	translation, err := r.t.Translate(
		c.Request.Context(),
		id,
		entity.Translation{
			Source:      body.Source,
			Destination: body.Destination,
			Original:    body.Original,
		},
	)
	if err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		errorResponse(c, http.StatusInternalServerError, "translation service problems")

		return
	}

	c.JSON(http.StatusOK, translation)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/controller/restapi/v1/translation.go
git commit -m "refactor(v1/translation): replace fiber ctx with gin ctx"
```

---

### Task 10: Update config and env example

**Files:**
- Modify: `config/config.go`
- Modify: `.env.example`

- [ ] **Step 1: Remove UsePreforkMode from config struct**

In `config/config.go`, find:
```go
http struct {
    Port           string `env:"HTTP_PORT,required"`
    UsePreforkMode bool   `env:"HTTP_USE_PREFORK_MODE" envDefault:"false"`
}
```

Replace with:
```go
http struct {
    Port string `env:"HTTP_PORT,required"`
}
```

- [ ] **Step 2: Remove PREFORK line from .env.example**

Find: `HTTP_USE_PREFORK_MODE=false`
Delete that line.

- [ ] **Step 3: Commit**

```bash
git add config/config.go .env.example
git commit -m "chore(config): remove fiber-specific prefork config"
```

---

### Task 11: Build, format, lint, and test

**Files:** None — verification step.

- [ ] **Step 1: Run go mod tidy**

Run: `go mod tidy`
Expected: exits cleanly

- [ ] **Step 2: Build the project**

Run: `go build ./cmd/app`
Expected: binary compiles without errors

- [ ] **Step 3: Run unit tests**

Run: `go test -v -race -count=1 ./internal/... ./pkg/...`
Expected: all tests pass (including migrated Auth middleware test)

- [ ] **Step 4: Run formatter**

Run: `make format`
Expected: no errors

- [ ] **Step 5: Run linter**

Run: `golangci-lint run`
Expected: no lint errors

- [ ] **Step 6: Commit any stray formatting/lint fixes**

```bash
git add -A
git commit -m "style: run formatter and fix lint issues"
```
