// Package app configures and runs application.
package app

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/controller/web"
	"github.com/minhhoccode111/fileuploader/internal/repo/persistent/user"
	"github.com/minhhoccode111/fileuploader/internal/repo/session"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	usecaseUser "github.com/minhhoccode111/fileuploader/internal/usecase/user"
	"github.com/minhhoccode111/fileuploader/pkg/httpserver"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/minio"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

type useCases struct {
	user usecase.User
}

type servers struct {
	http *httpserver.Server
}

func initUseCases(pg *postgres.Postgres, rdb *redis.Redis, sessionTTL time.Duration) useCases {
	userRepo := user.New(pg)
	sessionRepo := session.New(rdb)

	return useCases{
		user: usecaseUser.New(userRepo, sessionRepo, pg, sessionTTL),
	}
}

func initServers(
	cfg *config.Config,
	uc useCases,
	l logger.Interface,
) servers {
	// HTTP Server
	httpServer := httpserver.New(
		l,
		httpserver.Port(cfg.HTTP.Port),
		httpserver.Prefork(cfg.HTTP.UsePreforkMode),
	)
	// Validator
	v := validatorx.New()

	web.NewRouter(httpServer.App, uc.user, l, v, cfg)

	return servers{
		http: httpServer,
	}
}

func (s *servers) startServers() {
	s.http.Start()
}

func (s *servers) waitForShutdown(l logger.Interface) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-interrupt:
		l.Info("app - Run - signal: %s", sig.String())
	case err := <-s.http.Notify():
		l.Error(fmt.Errorf("app - Run - httpServer.Notify: %w", err))
	}

	s.shutdownServers(l)
}

func (s *servers) shutdownServers(l logger.Interface) {
	if err := s.http.Shutdown(); err != nil {
		l.Error(fmt.Errorf("app - Run - httpServer.Shutdown: %w", err))
	}
}

// Run creates objects via constructors.
func Run(cfg *config.Config) {
	l := logger.New(cfg.Log.Level)

	// Repository
	pg, err := postgres.New(cfg.PG.URL, postgres.MaxPoolSize(cfg.PG.PoolMax))
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - postgres.New: %w", err))
	}
	defer pg.Close()

	// Redis
	redisConn, err := redis.New(cfg.Redis.Addr)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - redis.New: %w", err))
	}
	defer redisConn.Close()

	// MinIO
	minioClient, err := minio.New(
		cfg.MinIO.Endpoint,
		cfg.MinIO.AccessKey,
		cfg.MinIO.SecretKey,
		minio.UseSSL(cfg.MinIO.UseSSL),
	)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - minio.New: %w", err))
	}
	defer minioClient.Close()

	uc := initUseCases(pg, redisConn, cfg.Session.TTL)
	s := initServers(cfg, uc, l)
	s.startServers()
	s.waitForShutdown(l)
}
