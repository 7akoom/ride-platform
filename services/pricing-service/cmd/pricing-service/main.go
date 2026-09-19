package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/events"
	outboxapp "github.com/7akoom/ride-platform/services/pricing-service/internal/application/outbox"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/config"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/clients"
	clockinfra "github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/database"
	natsinfra "github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/messaging/nats"
	postgresrepo "github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/routing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/infrastructure/weather"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/pricing-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// tripEventsStream is created by the compose bootstrap container that
	// belongs to trip-service; this service only binds a consumer to it.
	tripEventsStream = "TRIP_EVENTS"

	// tripCompletedDurable is this service's own durable consumer name. It
	// must stay stable across restarts so redelivery resumes where it left off.
	tripCompletedDurable = "pricing-trip-completed"
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

	autoFareConfig, err := config.ParseAutoFare(cfg)
	if err != nil {
		logger.Error("invalid auto-fare configuration", "error", err)

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

	locationConn, err := grpc.NewClient(
		cfg.LocationServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to location-service", "error", err)

		return 1
	}
	defer locationConn.Close()

	tripConn, err := grpc.NewClient(
		cfg.TripServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to trip-service", "error", err)

		return 1
	}
	defer tripConn.Close()

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

	pricingService := pricing.NewService(
		postgresrepo.NewPricingRepository(pool),
		clients.NewLocationClient(locationConn),
		routing.NewOSRMClient(cfg.OSRMBaseURL, cfg.RoutingTimeout),
		weather.NewClient(cfg.WeatherTimeout),
	)
	pricingHandler := grpcserver.NewPricingHandler(pricingService, logger)

	eventHandler := events.NewHandler(
		pricingService,
		clients.NewTripClient(tripConn),
		autoFareConfig.RetryInterval,
		autoFareConfig.GiveUpAfter,
		logger,
	)

	tripSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		tripEventsStream,
		tripCompletedDurable,
		[]string{events.SubjectTripCompleted},
		eventHandler.Handle,
		logger,
	)
	if err != nil {
		logger.Error("failed to subscribe to trip.completed events", "error", err)

		return 1
	}
	defer tripSubscription.Stop()

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
	server.RegisterPricingService(pricingHandler)

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
