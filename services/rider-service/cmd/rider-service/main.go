package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
	"github.com/7akoom/ride-platform/services/rider-service/internal/config"
	"github.com/7akoom/ride-platform/services/rider-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/rider-service/internal/infrastructure/identifier"
	postgresrepo "github.com/7akoom/ride-platform/services/rider-service/internal/infrastructure/persistence/postgres"
	grpcserver "github.com/7akoom/ride-platform/services/rider-service/internal/transport/grpc"
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

	riderRepository := postgresrepo.NewRiderRepository(pool)
	idGenerator := identifier.NewUUIDGenerator()

	riderService := rider.NewService(riderRepository, idGenerator)
	riderHandler := grpcserver.NewRiderHandler(riderService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterRiderService(riderHandler)

	// TODO: wire the outbox worker the same way identity-service does
	// (internal/application/outbox/worker.go + NATS JetStream publisher)
	// once Dispatch/Trip services are ready to consume rider.created events.

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
