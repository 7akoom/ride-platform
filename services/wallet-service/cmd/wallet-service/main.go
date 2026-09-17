package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/wallet-service/internal/application/outbox"
	topupapp "github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/config"
	clockinfra "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/database"
	natsinfra "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/messaging/nats"
	postgresrepo "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/token"
	zaincashinfra "github.com/7akoom/ride-platform/services/wallet-service/internal/infrastructure/zaincash"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/observability"
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

	if err := metricsRuntime.RegisterOutboxMetrics(outboxStore, logger); err != nil {
		logger.Error("failed to register outbox metrics", "error", err)

		return 1
	}

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

	zainCashClient := zaincashinfra.NewClient(zaincashinfra.Config{
		BaseURL:       cfg.ZainCashBaseURL,
		ClientID:      cfg.ZainCashClientID,
		ClientSecret:  cfg.ZainCashClientSecret,
		Scope:         cfg.ZainCashScope,
		WebhookSecret: cfg.ZainCashWebhookSecret,
	})
	zainCashAdapter := zaincashinfra.NewAdapter(zainCashClient)

	topupService := topupapp.NewService(
		postgresrepo.NewTopUpRepository(pool),
		zainCashAdapter,
		walletService,
		cfg.ZainCashSuccessURL,
		cfg.ZainCashFailureURL,
	)

	walletHandler := grpcserver.NewWalletHandler(walletService, topupService, logger)

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

		metricsShutdownCtx, cancelMetricsShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancelMetricsShutdown()

		if err := metricsRuntime.Shutdown(metricsShutdownCtx); err != nil {
			logger.Warn("failed to shut down metrics runtime cleanly", "error", err)
		}
	}

	<-outboxDone

	return 0
}
