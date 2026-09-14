package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/config"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/infrastructure/clients"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/infrastructure/token"
	grpcserver "github.com/7akoom/ride-platform/services/dispatch-service/internal/transport/grpc"
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

	tripConn, err := dialService(cfg.TripServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to trip-service", "error", err)

		return 1
	}
	defer tripConn.Close()

	locationConn, err := dialService(cfg.LocationServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to location-service", "error", err)

		return 1
	}
	defer locationConn.Close()

	driverConn, err := dialService(cfg.DriverServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to driver-service", "error", err)

		return 1
	}
	defer driverConn.Close()

	walletConn, err := dialService(cfg.WalletServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to wallet-service", "error", err)

		return 1
	}
	defer walletConn.Close()

	dispatchService := dispatch.NewService(
		clients.NewTripClient(tripConn),
		clients.NewLocationClient(locationConn),
		clients.NewDriverClient(driverConn),
		clients.NewWalletClient(walletConn),
	)
	dispatchHandler := grpcserver.NewDispatchHandler(dispatchService)

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

	server := grpcserver.NewServer(
		cfg.GRPCAddress,
		logger,
		grpcserver.NewAuthenticationUnaryInterceptor(accessTokenVerifier, cfg.InternalServiceToken),
	)
	server.RegisterDispatchService(dispatchHandler)

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

func dialService(address string, internalServiceToken string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(internalServiceToken),
		),
	)
}
