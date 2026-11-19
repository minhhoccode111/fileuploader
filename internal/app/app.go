// Package app configures and runs application.
package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/controller/web"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	fileRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/file"
	folderRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/folder"
	shareRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/share"
	userRepo "github.com/minhhoccode111/fileuploader/internal/repo/persistent/user"
	"github.com/minhhoccode111/fileuploader/internal/repo/session"
	transferRepo "github.com/minhhoccode111/fileuploader/internal/repo/transfer"
	uploadRepo "github.com/minhhoccode111/fileuploader/internal/repo/upload"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	usecaseFile "github.com/minhhoccode111/fileuploader/internal/usecase/file"
	usecaseFolder "github.com/minhhoccode111/fileuploader/internal/usecase/folder"
	usecaseShare "github.com/minhhoccode111/fileuploader/internal/usecase/share"
	usecaseUser "github.com/minhhoccode111/fileuploader/internal/usecase/user"
	"github.com/minhhoccode111/fileuploader/pkg/httpserver"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/minio"
	"github.com/minhhoccode111/fileuploader/pkg/postgres"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

type useCases struct {
	user   usecase.User
	folder usecase.Folder
	file   usecase.File
	share  usecase.Share
}

type servers struct {
	http *httpserver.Server
}

func initUseCases(
	pg *postgres.Postgres,
	rdb *redis.Redis,
	storage repo.ObjectStorage,
	cfg *config.Config,
) useCases {
	users := userRepo.New(pg)
	sessionRepo := session.New(rdb)
	folders := folderRepo.New(pg)
	files := fileRepo.New(pg)
	shares := shareRepo.New(pg)
	uploads := uploadRepo.New(rdb)
	pacer := transferRepo.New(rdb)
	limits := usecase.Limits{
		StorageQuotaBytes:       cfg.Limits.StorageQuotaBytes,
		DownloadRateBytesPerSec: cfg.Limits.DownloadRateBytesPerSec,
	}

	return useCases{
		user:   usecaseUser.New(users, sessionRepo, folders, pg, cfg.Session.TTL),
		folder: usecaseFolder.New(users, folders, files, storage, pg, pacer, cfg.MinIO.Bucket, limits),
		file: usecaseFile.New(
			users, files, folders, storage, pg, cfg.MinIO.Bucket,
			uploads, pacer, limits,
			usecase.UploadSettings{
				MaxBytes:                     cfg.Upload.MaxBytes,
				PartBytes:                    cfg.Upload.PartBytes,
				SessionIdleTTL:               cfg.Upload.SessionIdleTTL,
				DefaultUploadRateBytesPerSec: cfg.Limits.UploadRateBytesPerSec,
			},
		),
		share: usecaseShare.New(shares, folders, files, pg),
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
		httpserver.BodyLimit(int(cfg.Upload.PartBytes+config.UploadOverheadBytes)),
		httpserver.ReadTimeout(cfg.Upload.Timeout),
	)
	// Validator
	v := validatorx.New()

	web.NewRouter(httpServer.App, uc.user, uc.folder, uc.file, uc.share, l, v, cfg)

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
		minio.PublicEndpoint(cfg.MinIO.PublicEndpoint),
	)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - minio.New: %w", err))
	}
	defer minioClient.Close()

	// Bucket
	err = minioClient.EnsureBucket(context.Background(), cfg.MinIO.Bucket)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - minioClient.EnsureBucket: %w", err))
	}

	uc := initUseCases(pg, redisConn, minioClient, cfg)

	sweepCtx, stopSweep := context.WithCancel(context.Background())
	defer stopSweep()

	startUploadSweeper(sweepCtx, uc.file, l)

	s := initServers(cfg, uc, l)
	s.startServers()
	s.waitForShutdown(l)
}
