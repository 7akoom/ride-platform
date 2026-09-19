package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/ingest"
	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/config"
	natsinfra "github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/messaging/nats"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/analytics/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/analytics/internal/transport/grpc"
)

// Durable consumer names — stable across restarts/redeploys so a durable
// consumer's delivery position is preserved on the stream. One per source
// stream this service consumes from; every subject on each stream is
// forwarded to the same ingest.Handler.Dispatch, which switches on the
// event's own event_type.
const (
	riderEventsDurable    = "analytics-rider-events"
	driverEventsDurable   = "analytics-driver-events"
	tripEventsDurable     = "analytics-trip-events"
	pricingEventsDurable  = "analytics-pricing-events"
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

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)

		return 1
	}
	defer pool.Close()

	writer := postgres.NewWriter(pool)
	reader := postgres.NewReader(pool)
	queryService := query.NewService(reader)
	analyticsHandler := grpcserver.NewAnalyticsHandler(queryService, logger)

	ingestHandler := ingest.NewHandler(writer)

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

	riderSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		"RIDER_EVENTS",
		riderEventsDurable,
		[]string{"rider.>"},
		ingestHandler.Dispatch,
		logger,
	)
	if err != nil {
		logger.Error("failed to subscribe to rider events", "error", err)

		return 1
	}
	defer riderSubscription.Stop()

	driverSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		"DRIVER_EVENTS",
		driverEventsDurable,
		[]string{"driver.>"},
		ingestHandler.Dispatch,
		logger,
	)
	if err != nil {
		logger.Error("failed to subscribe to driver events", "error", err)

		return 1
	}
	defer driverSubscription.Stop()

	tripSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		"TRIP_EVENTS",
		tripEventsDurable,
		[]string{"trip.>"},
		ingestHandler.Dispatch,
		logger,
	)
	if err != nil {
		logger.Error("failed to subscribe to trip events", "error", err)

		return 1
	}
	defer tripSubscription.Stop()

	pricingSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		"PRICING_EVENTS",
		pricingEventsDurable,
		[]string{"fare.>"},
		ingestHandler.Dispatch,
		logger,
	)
	if err != nil {
		logger.Error("failed to subscribe to pricing events", "error", err)

		return 1
	}
	defer pricingSubscription.Stop()

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
		grpcserver.NewAuthorizationUnaryInterceptor(),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
	)
	server.RegisterAnalyticsService(analyticsHandler)

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
	}()

	go func() {
		if err := metricsRuntime.Serve(); err != nil {
			// Non-fatal by design, matching every other service's MVP
			// trade-off: a metrics endpoint problem doesn't take down the
			// query API or event ingestion.
			logger.Error("metrics server exited with error", "error", err)
		}
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("gRPC server exited with error", "error", err)

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

	return 0
}
