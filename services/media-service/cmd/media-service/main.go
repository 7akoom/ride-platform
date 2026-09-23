package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
	"github.com/7akoom/ride-platform/services/media-service/internal/config"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/clients"
	clockinfra "github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/identifier"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/objectstore"
	postgresrepo "github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/media-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/media-service/internal/transport/grpc"
)

const (
	maintenanceInterval = time.Minute
	bucketWaitTimeout   = 60 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Load()

	if err := config.ValidateSecrets(cfg); err != nil {
		slog.Error("refusing to start with an unsafe configuration", "error", err)

		return 1
	}

	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	).With(
		"service", cfg.ServiceName,
		"environment", cfg.Environment,
	)

	limits, err := config.ParseLimits(cfg)
	if err != nil {
		logger.Error("invalid media limits", "error", err)

		return 1
	}

	metricsRuntime, err := observability.NewMetricsRuntime(cfg.ServiceName, cfg.MetricsAddress)
	if err != nil {
		logger.Error("invalid metrics configuration", "error", err)

		return 1
	}

	metricsInterceptor, err := metricsRuntime.UnaryServerInterceptor()
	if err != nil {
		logger.Error("failed to configure rpc metrics interceptor", "error", err)

		return 1
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)

		return 1
	}
	defer pool.Close()

	store, err := objectstore.New(objectstore.Config{
		Endpoint:       cfg.S3Endpoint,
		PublicEndpoint: cfg.S3PublicEndpoint,
		Region:         cfg.S3Region,
		Bucket:         cfg.S3Bucket,
		AccessKey:      cfg.S3AccessKey,
		SecretKey:      cfg.S3SecretKey,
	})
	if err != nil {
		logger.Error("invalid object store configuration", "error", err)

		return 1
	}

	if err := ensureBucket(ctx, store, logger); err != nil {
		logger.Error("the object store is not usable", "bucket", cfg.S3Bucket, "error", err)

		return 1
	}

	mediaService := media.NewService(
		postgresrepo.NewMediaRepository(pool),
		objectstore.MediaStore{Store: store},
		identifier.NewUUIDGenerator(),
		clockinfra.NewSystemClock(),
		media.Settings{
			UploadURLTTL:             limits.UploadURLTTL,
			DownloadURLTTL:           limits.DownloadURLTTL,
			PendingTTL:               limits.PendingUploadTTL,
			MaxPendingPerOwner:       limits.MaxPendingPerOwner,
			MaxConcurrentInspections: limits.MaxConcurrentInspections,
		},
		logger,
	)

	accessTokenVerifier, err := token.NewAccessTokenVerifier(
		cfg.AccessTokenPublicKeyPath,
		cfg.AccessTokenIssuer,
		cfg.AccessTokenAudience,
		cfg.AccessTokenKeyID,
	)
	if err != nil {
		logger.Error("invalid access token verifier configuration", "error", err)

		return 1
	}

	staffConn, err := grpc.NewClient(
		cfg.StaffServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to staff-service", "error", err)

		return 1
	}
	defer staffConn.Close()

	rateLimitConfig, err := config.ParseRateLimit(cfg)
	if err != nil {
		logger.Error("invalid rate limit configuration", "error", err)

		return 1
	}

	server := grpcserver.NewServer(
		cfg.GRPCAddress,
		logger,
		metricsInterceptor,
		grpcserver.NewAuthenticationUnaryInterceptor(accessTokenVerifier, cfg.InternalServiceToken),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
		grpcserver.NewAuthorizationUnaryInterceptor(mediaService, clients.NewStaffAuthorizer(staffConn, logger)),
	)
	server.RegisterMediaService(grpcserver.NewMediaHandler(mediaService, logger))

	go mediaService.RunMaintenance(ctx, maintenanceInterval)

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
	}()

	go func() {
		if err := metricsRuntime.Serve(); err != nil {
			logger.Error("metrics server exited with error", "error", err)
		}
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("gRPC server exited with error", "error", err)

			return 1
		}

	case <-ctx.Done():
		logger.Info("shutdown signal received, stopping gRPC server")

		const gracefulShutdownTimeout = 10 * time.Second

		shutdownDone := make(chan struct{})

		go func() {
			defer close(shutdownDone)
			server.GracefulStop()
		}()

		select {
		case <-shutdownDone:
		case <-time.After(gracefulShutdownTimeout):
			logger.Warn("graceful shutdown timed out; forcing stop")
			server.Stop()
		}

		metricsShutdownCtx, cancelMetricsShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancelMetricsShutdown()

		if err := metricsRuntime.Shutdown(metricsShutdownCtx); err != nil {
			logger.Warn("failed to shut down metrics runtime cleanly", "error", err)
		}
	}

	return 0
}

// ensureBucket creates the bucket if needed, retrying while the object store
// is still starting.
func ensureBucket(ctx context.Context, store *objectstore.Store, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, bucketWaitTimeout)
	defer cancel()

	for {
		err := store.EnsureBucket(ctx)
		if err == nil {
			return nil
		}

		logger.Warn("object store not ready yet", "error", err)

		select {
		case <-ctx.Done():
			return err
		case <-time.After(2 * time.Second):
		}
	}
}
