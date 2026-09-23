package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	// City time zones are checked against the IANA database; embedding it keeps
	// that working in images without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/city"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/place"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"github.com/7akoom/ride-platform/services/location-service/internal/config"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/clients"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/geocoding"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/identifier"
	postgresrepo "github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/persistence/postgres"
	valkeystore "github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/persistence/valkey"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/routing"
	"github.com/7akoom/ride-platform/services/location-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/location-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/location-service/internal/transport/grpc"
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

	valkeyClient, err := database.NewValkeyClient(ctx, cfg.ValkeyAddress, cfg.ValkeyPassword)
	if err != nil {
		logger.Error("failed to connect to Valkey", "error", err)

		return 1
	}
	defer valkeyClient.Close()

	locationRepository := valkeystore.NewLocationStore(valkeyClient)
	locationService := location.NewService(locationRepository)

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to PostgreSQL", "error", err)

		return 1
	}
	defer pool.Close()

	zoneRepository := postgresrepo.NewZoneStore(pool)
	zoneService := zone.NewService(zoneRepository, identifier.NewUUIDGenerator())

	mapsConfig, err := config.ParseMaps(cfg)
	if err != nil {
		logger.Error("invalid maps configuration", "error", err)

		return 1
	}

	cityService := city.NewService(postgresrepo.NewCityStore(pool), identifier.NewUUIDGenerator())
	placeService := place.NewService(postgresrepo.NewPlaceStore(pool), identifier.NewUUIDGenerator())

	// Curated places come before map results in search.
	mapsService := maps.NewServiceWithCurated(
		routing.NewOSRMClient(mapsConfig.OSRMBaseURL, mapsConfig.Timeout),
		geocoding.NewNominatimClient(mapsConfig.NominatimBaseURL, mapsConfig.CountryCodes, mapsConfig.Timeout),
		placeService,
		logger,
	)

	locationHandler := grpcserver.NewLocationHandler(locationService, zoneService, logger).
		WithMaps(mapsService).
		WithCatalog(cityService, placeService)

	riderConn, err := grpc.NewClient(
		cfg.RiderServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to rider-service", "error", err)

		return 1
	}
	defer riderConn.Close()

	driverConn, err := grpc.NewClient(
		cfg.DriverServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to driver-service", "error", err)

		return 1
	}
	defer driverConn.Close()

	profileResolver := grpcserver.NewCachingResolver(clients.NewProfileResolver(riderConn, driverConn))

	staffConn, err := grpc.NewClient(
		cfg.StaffServiceAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			clients.ServiceAuthUnaryClientInterceptor(cfg.InternalServiceToken),
		),
	)
	if err != nil {
		logger.Error("failed to connect to staff-service", "error", err)

		return 1
	}
	defer staffConn.Close()

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
		grpcserver.NewAuthorizationUnaryInterceptor(profileResolver, clients.NewStaffAuthorizer(staffConn, logger)),
		grpcserver.NewRateLimitUnaryInterceptor(rateLimitConfig.RequestsPerSecond, rateLimitConfig.Burst),
	)
	server.RegisterLocationService(locationHandler)

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
		server.GracefulStop()

		metricsShutdownCtx, cancelMetricsShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelMetricsShutdown()

		if err := metricsRuntime.Shutdown(metricsShutdownCtx); err != nil {
			logger.Warn("failed to shut down metrics runtime cleanly", "error", err)
		}
	}

	return 0
}
