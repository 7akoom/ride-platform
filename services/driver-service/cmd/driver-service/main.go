package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	"github.com/7akoom/ride-platform/services/driver-service/internal/config"
	"github.com/7akoom/ride-platform/services/driver-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/driver-service/internal/infrastructure/identifier"
	postgresrepo "github.com/7akoom/ride-platform/services/driver-service/internal/infrastructure/persistence/postgres"
	grpcserver "github.com/7akoom/ride-platform/services/driver-service/internal/transport/grpc"
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

	driverRepository := postgresrepo.NewDriverRepository(pool)
	idGenerator := identifier.NewUUIDGenerator()

	driverService := driver.NewService(driverRepository, idGenerator)
	driverHandler := grpcserver.NewDriverHandler(driverService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterDriverService(driverHandler)

	// TODO: wire the outbox worker the same way identity-service does
	// once Dispatch/Trip services are ready to consume driver.created
	// and driver availability change events.

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
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
	}

	return 0
}
