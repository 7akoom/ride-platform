package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
	"github.com/7akoom/ride-platform/services/notification-service/internal/config"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/channels"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/clients"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/database"
	natsinfra "github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/messaging/nats"
	postgresrepo "github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/notification-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/notification-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Durable consumer names — stable across restarts/redeploys so a durable
// consumer's delivery position is preserved on the stream.
const (
	tripEventsDurable    = "notification-trip-events"
	pricingEventsDurable = "notification-pricing-events"
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

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)

		return 1
	}
	defer pool.Close()

	tripConn, err := dialService(cfg.TripServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to trip-service", "error", err)

		return 1
	}
	defer tripConn.Close()

	driverConn, err := dialService(cfg.DriverServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to driver-service", "error", err)

		return 1
	}
	defer driverConn.Close()

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

	pushSender := buildPushSender(cfg, logger)

	notificationService := notification.NewService(
		postgresrepo.NewNotificationRepository(pool),
		pushSender,
		// SMS gateways are regional; wire a local provider here when
		// the deployment has one.
		channels.NewNoopSMSSender(),
	)
	notificationHandler := grpcserver.NewNotificationHandler(notificationService)

	eventHandler := events.NewHandler(
		notificationService,
		clients.NewTripClient(tripConn),
		clients.NewDriverClient(driverConn),
		logger,
	)

	tripSubscription, err := natsinfra.SubscribeDurable(
		ctx,
		natsConnection.JetStream(),
		"TRIP_EVENTS",
		tripEventsDurable,
		[]string{"trip.accepted", "trip.started", "trip.cancelled"},
		eventHandler.Dispatch,
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
		[]string{"fare.calculated"},
		eventHandler.Dispatch,
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
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
	)
	server.RegisterNotificationService(notificationHandler)

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

// buildPushSender degrades to the no-op sender rather than refusing to
// start. A deployment without Firebase credentials is a perfectly valid
// state — in-app notifications keep working, and push deliveries are
// recorded as unconfigured instead of the whole service being down.
func buildPushSender(cfg config.Config, logger *slog.Logger) notification.PushSender {
	if cfg.FCMCredentialsFile == "" {
		logger.Warn("FCM credentials not configured; push notifications disabled")

		return channels.NewNoopPushSender()
	}

	raw, err := os.ReadFile(cfg.FCMCredentialsFile)
	if err != nil {
		logger.Error("failed to read FCM credentials; push disabled", "error", err)

		return channels.NewNoopPushSender()
	}

	account, err := channels.LoadServiceAccount(raw)
	if err != nil {
		logger.Error("failed to parse FCM credentials; push disabled", "error", err)

		return channels.NewNoopPushSender()
	}

	sender, err := channels.NewFCMSender(account, cfg.PushTimeout)
	if err != nil {
		logger.Error("failed to initialise FCM sender; push disabled", "error", err)

		return channels.NewNoopPushSender()
	}

	logger.Info("FCM push sender configured", "project_id", account.ProjectID)

	return sender
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
