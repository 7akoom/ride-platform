package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/config"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/clients"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/database"
	postgresrepo "github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/routing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/weather"
	grpcserver "github.com/7akoom/ride-platform/services/pricing-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	locationConn, err := grpc.NewClient(
		cfg.LocationServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		logger.Error("failed to connect to location-service", "error", err)

		return 1
	}
	defer locationConn.Close()

	pricingService := pricing.NewService(
		postgresrepo.NewPricingRepository(pool),
		clients.NewLocationClient(locationConn),
		routing.NewOSRMClient(cfg.OSRMBaseURL, cfg.RoutingTimeout),
		weather.NewClient(cfg.WeatherTimeout),
	)
	pricingHandler := grpcserver.NewPricingHandler(pricingService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterPricingService(pricingHandler)

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
