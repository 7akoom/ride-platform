package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"github.com/7akoom/ride-platform/services/location-service/internal/config"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/identifier"
	postgresrepo "github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/persistence/postgres"
	valkeystore "github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/persistence/valkey"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/location-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/location-service/internal/transport/grpc"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Load()

	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	).With(
		"service", cfg.ServiceName,
		"environment", cfg.Environment,
	)

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

	valkeyClient, err := database.NewValkeyClient(ctx, cfg.ValkeyAddress, cfg.ValkeyPassword)
	if err != nil {
		logger.Error("failed to connect to Valkey", "error", err)

		return 1
	}
	defer valkeyClient.Close()

	locationRepository := valkeystore.NewLocationStore(valkeyClient)
	locationService := location.NewService(locationRepository)

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to PostgreSQL", "error", err)

		return 1
	}
	defer pool.Close()

	zoneRepository := postgresrepo.NewZoneStore(pool)
	zoneService := zone.NewService(zoneRepository, identifier.NewUUIDGenerator())

	locationHandler := grpcserver.NewLocationHandler(locationService, zoneService, logger)

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
	)
	server.RegisterLocationService(locationHandler)

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
	}()

	go func() {
		if err := metricsRuntime.Serve(); err != nil {
			// Non-fatal by design: an MVP trade-off, so a metrics
			// endpoint problem (e.g. port already in use) doesn't take
			// down trip-serving traffic. Revisit if this ever needs to
			// be a hard dependency.
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
		server.GracefulStop()

		metricsShutdownCtx, cancelMetricsShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelMetricsShutdown()

		if err := metricsRuntime.Shutdown(metricsShutdownCtx); err != nil {
			logger.Warn("failed to shut down metrics runtime cleanly", "error", err)
		}
	}

	return 0
}
