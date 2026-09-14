package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"github.com/7akoom/ride-platform/services/trip-service/internal/config"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/identifier"
	postgresrepo "github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/persistence/postgres"
	grpcserver "github.com/7akoom/ride-platform/services/trip-service/internal/transport/grpc"
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

	tripRepository := postgresrepo.NewTripRepository(pool)
	idGenerator := identifier.NewUUIDGenerator()

	tripService := trip.NewService(tripRepository, idGenerator)
	tripHandler := grpcserver.NewTripHandler(tripService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterTripService(tripHandler)

	// TODO: wire the outbox worker + NATS publisher once Dispatch (to
	// consume trip.requested), Wallet (trip.completed), and Notification
	// are ready to react to trip events.

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
