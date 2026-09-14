package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/config"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/database"
	postgresrepo "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/persistence/postgres"
	grpcserver "github.com/7akoom/ride-platform/services/wallet-service/internal/transport/grpc"
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

	walletService := wallet.NewService(postgresrepo.NewWalletRepository(pool))
	walletHandler := grpcserver.NewWalletHandler(walletService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterWalletService(walletHandler)

	// TODO: wire the outbox worker so trip.settled reaches Notification,
	// and subscribe to trip.completed so settlement happens automatically
	// instead of being called explicitly. Part of the same deferred
	// NATS pass as every other service.

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
