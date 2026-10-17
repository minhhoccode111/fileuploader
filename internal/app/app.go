// Package app configures and runs application.
package app

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evrone/go-clean-template/config"
	"github.com/evrone/go-clean-template/internal/controller/web"
	"github.com/evrone/go-clean-template/internal/repo/persistent/user"
	"github.com/evrone/go-clean-template/internal/repo/session"
	"github.com/evrone/go-clean-template/internal/usecase"
	usecaseUser "github.com/evrone/go-clean-template/internal/usecase/user"
	"github.com/evrone/go-clean-template/pkg/httpserver"
	"github.com/evrone/go-clean-template/pkg/logger"
	"github.com/evrone/go-clean-template/pkg/postgres"
	"github.com/evrone/go-clean-template/pkg/redis"
	"github.com/evrone/go-clean-template/pkg/validatorx"
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
		user: usecaseUser.New(userRepo, sessionRepo, sessionTTL),
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

	uc := initUseCases(pg, redisConn, cfg.Session.TTL)
	s := initServers(cfg, uc, l)
	s.startServers()
	s.waitForShutdown(l)
}
