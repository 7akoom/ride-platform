package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/wallet-service/internal/application/outbox"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/config"
	clockinfra "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/database"
	natsinfra "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/messaging/nats"
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

	natsConfig, err := config.ParseNATS(cfg)
	if err != nil {
		logger.Error("invalid NATS configuration", "error", err)

		return 1
	}

	outboxConfig, err := config.ParseOutbox(cfg)
	if err != nil {
		logger.Error("invalid outbox configuration", "error", err)

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

	natsConnection, err := natsinfra.OpenConnection(
		natsinfra.ConnectionConfig{
			URL:            natsConfig.URL,
			ClientName:     natsConfig.ClientName,
			ConnectTimeout: natsConfig.ConnectTimeout,
			ReconnectWait:  natsConfig.ReconnectWait,
			DrainTimeout:   natsConfig.DrainTimeout,
		},
		logger,
	)
	if err != nil {
		logger.Error("failed to configure NATS connection", "error", err)

		return 1
	}

	defer func() {
		if err := natsConnection.Drain(); err != nil {
			logger.Warn("failed to drain NATS connection", "error", err)
		}
	}()

	outboxStore := postgresrepo.NewOutboxStore(pool)

	outboxPublisher := natsinfra.NewJetStreamPublisher(
		natsConnection.JetStream(),
		natsConfig.PublishTimeout,
	)

	outboxProcessor := outboxapp.NewProcessor(
		outboxStore,
		outboxPublisher,
		clockinfra.NewSystemClock(),
		outboxapp.ProcessorConfig{
			BatchSize:         outboxConfig.BatchSize,
			LeaseDuration:     outboxConfig.LeaseDuration,
			InitialRetryDelay: outboxConfig.InitialRetryDelay,
			MaxRetryDelay:     outboxConfig.MaxRetryDelay,
		},
	)

	outboxWorker := outboxapp.NewWorker(
		outboxProcessor,
		logger,
		outboxapp.WorkerConfig{
			PollInterval: outboxConfig.PollInterval,
		},
	)

	walletService := wallet.NewService(postgresrepo.NewWalletRepository(pool))
	walletHandler := grpcserver.NewWalletHandler(walletService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterWalletService(walletHandler)

	outboxDone := make(chan struct{})

	go func() {
		defer close(outboxDone)

		if err := outboxWorker.Run(ctx); err != nil {
			logger.Error("outbox worker stopped with error", "error", err)
		}
	}()

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("gRPC server exited with error", "error", err)

			<-outboxDone

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
	}

	<-outboxDone

	return 0
}
