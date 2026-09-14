package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/config"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/database"
	valkeystore "github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/persistence/valkey"
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
	locationHandler := grpcserver.NewLocationHandler(locationService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterLocationService(locationHandler)

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
