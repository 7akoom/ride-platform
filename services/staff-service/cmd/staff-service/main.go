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

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
	"github.com/7akoom/ride-platform/services/staff-service/internal/config"
	clockinfra "github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/identifier"
	"github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/identity"
	postgresrepo "github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/staff-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/staff-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/staff-service/internal/transport/grpc"
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

	// No internal-token interceptor here: identity-service only accepts the
	// caller's own access token, which the client attaches per call.
	identityConn, err := grpc.NewClient(
		cfg.IdentityServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		logger.Error("failed to connect to identity-service", "error", err)

		return 1
	}
	defer identityConn.Close()

	staffService := staff.NewService(
		postgresrepo.NewStaffRepository(pool),
		identity.NewClient(identityConn),
		identifier.NewUUIDGenerator(),
		clockinfra.NewSystemClock(),
	)

	invited, err := staffService.Bootstrap(ctx, cfg.BootstrapOwnerEmail)
	if err != nil {
		logger.Error("failed to invite the first owner", "error", err)

		return 1
	}

	if invited {
		logger.Info("invited the first owner; sign in with that email address and accept the invitation")
	}

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
		grpcserver.NewAuthorizationUnaryInterceptor(staffService, logger),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
	)
	server.RegisterStaffService(grpcserver.NewStaffHandler(staffService, logger))

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
