package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	outboxapp "github.com/7akoom/ride-platform/services/support-service/internal/application/outbox"
	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
	"github.com/7akoom/ride-platform/services/support-service/internal/config"
	"github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/clients"
	clockinfra "github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/identifier"
	natsinfra "github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/messaging/nats"
	postgresrepo "github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/support-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/support-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/support-service/internal/transport/grpc"
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

	desk, err := config.ParseDesk(cfg)
	if err != nil {
		logger.Error("invalid support desk configuration", "error", err)

		return 1
	}

	rateLimitConfig, err := config.ParseRateLimit(cfg)
	if err != nil {
		logger.Error("invalid rate limit configuration", "error", err)

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

	outboxWorker := outboxapp.NewWorker(
		outboxapp.NewProcessor(
			outboxStore,
			natsinfra.NewJetStreamPublisher(natsConnection.JetStream(), natsConfig.PublishTimeout),
			clockinfra.NewSystemClock(),
			outboxapp.ProcessorConfig{
				BatchSize:         outboxConfig.BatchSize,
				LeaseDuration:     outboxConfig.LeaseDuration,
				InitialRetryDelay: outboxConfig.InitialRetryDelay,
				MaxRetryDelay:     outboxConfig.MaxRetryDelay,
			},
		),
		logger,
		outboxapp.WorkerConfig{PollInterval: outboxConfig.PollInterval},
	)

	// Every peer is called as a service (internal token).
	conns := map[string]*grpc.ClientConn{}

	for name, address := range map[string]string{
		"staff-service":    cfg.StaffServiceAddress,
		"media-service":    cfg.MediaServiceAddress,
		"trip-service":     cfg.TripServiceAddress,
		"rider-service":    cfg.RiderServiceAddress,
		"driver-service":   cfg.DriverServiceAddress,
		"wallet-service":   cfg.WalletServiceAddress,
		"identity-service": cfg.IdentityServiceAddress,
	} {
		conn, err := grpc.NewClient(
			address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken)),
		)
		if err != nil {
			logger.Error("failed to connect to a peer service", "service", name, "error", err)

			return 1
		}

		defer conn.Close()

		conns[name] = conn
	}

	staffAuthorizer := clients.NewStaffAuthorizer(conns["staff-service"], logger)

	supportService := support.NewService(
		support.Dependencies{
			Repository: postgresrepo.NewSupportRepository(pool),
			Trips:      clients.NewTrips(conns["trip-service"]),
			Profiles:   clients.NewProfiles(conns["rider-service"], conns["driver-service"]),
			Media:      clients.NewMedia(conns["media-service"]),
			Wallet:     clients.NewWallet(conns["wallet-service"]),
			Accounts:   clients.NewAccounts(conns["identity-service"]),
			Staff:      staffAuthorizer,
			IDs:        identifier.NewUUIDGenerator(),
			Clock:      clockinfra.NewSystemClock(),
		},
		support.Config{
			RefundLimit:      desk.RefundLimit,
			MaxOpenTickets:   desk.MaxOpenTickets,
			TripMaxAge:       desk.TripMaxAge,
			FirstResponse:    firstResponseTargets(desk.FirstResponse),
			AutoResolveAfter: desk.AutoResolveAfter,
			AutoCloseAfter:   desk.AutoCloseAfter,
		},
		logger,
	)

	sosSubscription, err := subscribeTripSOS(ctx, natsConnection.JetStream(), supportService.HandleSOSEvent, logger)
	if err != nil {
		logger.Error("failed to subscribe to trip.sos_triggered events", "error", err)

		return 1
	}
	defer sosSubscription.Stop()

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
		metricsInterceptor,
		grpcserver.NewAuthenticationUnaryInterceptor(accessTokenVerifier, cfg.InternalServiceToken),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
		grpcserver.NewAuthorizationUnaryInterceptor(staffAuthorizer),
	)
	server.RegisterSupportService(grpcserver.NewSupportHandler(supportService, logger))

	outboxDone := make(chan struct{})

	go func() {
		defer close(outboxDone)

		if err := outboxWorker.Run(ctx); err != nil {
			logger.Error("outbox worker stopped with error", "error", err)
		}
	}()

	workersDone := make(chan struct{})

	go func() {
		defer close(workersDone)
		supportService.RunWorkers(ctx, desk.WorkerInterval)
	}()

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
	}()

	go func() {
		if err := metricsRuntime.Serve(); err != nil {
			logger.Error("metrics server exited with error", "error", err)
		}
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("gRPC server exited with error", "error", err)

			stop()
			<-outboxDone
			<-workersDone

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
	<-workersDone

	return 0
}

func firstResponseTargets(byName map[string]time.Duration) map[support.Priority]time.Duration {
	out := make(map[support.Priority]time.Duration, len(byName))
	for name, d := range byName {
		out[support.Priority(name)] = d
	}

	return out
}
