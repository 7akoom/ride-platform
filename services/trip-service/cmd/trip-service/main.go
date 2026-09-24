package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/trip-service/internal/application/outbox"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"github.com/7akoom/ride-platform/services/trip-service/internal/config"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/clients"
	clockinfra "github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/identifier"
	natsinfra "github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/messaging/nats"
	postgresrepo "github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/trip-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/trip-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Load()

	if err := config.ValidateSecrets(cfg); err != nil {
		slog.Error("refusing to start with an unsafe configuration", "error", err)

		return 1
	}

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

	noShowWait, err := config.ParseNoShowWait(cfg)
	if err != nil {
		logger.Error("invalid no-show configuration", "error", err)

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

	locationConn, err := dialService(cfg.LocationServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to configure location-service connection", "error", err)

		return 1
	}
	defer locationConn.Close()

	riderConn, err := dialService(cfg.RiderServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to rider-service", "error", err)

		return 1
	}
	defer riderConn.Close()

	driverConn, err := dialService(cfg.DriverServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to driver-service", "error", err)

		return 1
	}
	defer driverConn.Close()

	mediaConn, err := dialService(cfg.MediaServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to media-service", "error", err)

		return 1
	}
	defer mediaConn.Close()

	// pricing-service calls back into this service (GetTrip) when a trip
	// completes; the connection is lazy, so neither has to start first.
	pricingConn, err := dialService(cfg.PricingServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to pricing-service", "error", err)

		return 1
	}
	defer pricingConn.Close()

	// wallet-service calls this service too (GetTrip); the connection is
	// lazy, so neither has to start first.
	walletConn, err := dialService(cfg.WalletServiceAddress, cfg.InternalServiceToken)
	if err != nil {
		logger.Error("failed to connect to wallet-service", "error", err)

		return 1
	}
	defer walletConn.Close()

	profileResolver := grpcserver.NewCachingResolver(clients.NewProfileResolver(riderConn, driverConn))

	locationClient := clients.NewLocationClient(locationConn)
	driverDirectory := clients.NewDriverDirectory(driverConn)

	tripRepository := postgresrepo.NewTripRepository(pool)
	idGenerator := identifier.NewUUIDGenerator()

	// WithSavedAddresses wraps the base service directly, so every other
	// decorator's RequestTrip reaches it.
	baseService := trip.WithSavedAddresses(
		trip.NewService(tripRepository, idGenerator, locationClient,
			trip.WithQuotes(clients.NewQuoteBook(pricingConn)),
			trip.WithRiderStanding(clients.NewRiderStanding(walletConn, logger)),
			trip.WithNoShowWait(noShowWait),
		),
		clients.NewAddressBook(riderConn),
	)

	tripService := trip.WithDriverArrival(trip.WithRecentDestinations(
		trip.WithPickupPhotos(
			trip.WithTripOffers(
				trip.WithTripHistory(
					trip.WithDriverTracking(
						trip.WithDriverProfile(baseService, driverDirectory),
						locationClient,
					),
					tripRepository,
				),
				tripRepository,
			),
			clients.NewPhotoLinks(mediaConn),
		),
		tripRepository,
	), locationClient, tripRepository)
	tripHandler := grpcserver.NewTripHandler(tripService, logger, grpcserver.WithParticipants(profileResolver))

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
		grpcserver.NewAuthorizationUnaryInterceptor(profileResolver, tripService),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
	)
	server.RegisterTripService(tripHandler)

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

func dialService(address string, internalServiceToken string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(internalServiceToken),
		),
	)
}
