package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	cleanupinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/cleanup"
	clockinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/identifier"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	postgresrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/postgres"
	valkeyrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/valkey"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/token"
	"github.com/7akoom/ride-platform/services/identity-service/internal/observability"
	grpcserver "github.com/7akoom/ride-platform/services/identity-service/internal/transport/grpc"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Load()

	logger := observability.NewLogger(
		cfg.ServiceName,
		cfg.Environment,
	)

	durations, err := config.ParseDurations(cfg)
	if err != nil {
		logger.Error(
			"invalid duration configuration",
			"error", err,
		)
		return 1
	}

	otpRequestRateLimit, err :=
		config.ParseOTPRequestRateLimit(cfg)
	if err != nil {
		logger.Error(
			"invalid OTP request rate limit configuration",
			"error", err,
		)
		return 1
	}

	otpHasher, err := otp.NewHasher(cfg.OTPHashSecret)
	if err != nil {
		logger.Error(
			"invalid OTP hash configuration",
			"error", err,
		)
		return 1
	}

	otpDelivery, err := otp.NewDevelopmentDelivery(
		cfg.Environment,
		logger,
	)
	if err != nil {
		logger.Error(
			"failed to configure OTP delivery",
			"error", err,
		)
		return 1
	}

	ctx := context.Background()

	databasePool, err := database.NewPostgresPool(
		ctx,
		cfg.DatabaseURL,
	)
	if err != nil {
		logger.Error(
			"failed to connect to PostgreSQL",
			"error", err,
		)
		return 1
	}
	defer databasePool.Close()

	logger.Info("PostgreSQL connection established")

	valkeyClient, err := database.NewValkeyClient(
		ctx,
		cfg.ValkeyAddress,
		cfg.ValkeyPassword,
	)
	if err != nil {
		logger.Error(
			"failed to connect to Valkey",
			"error", err,
		)
		return 1
	}
	defer valkeyClient.Close()

	logger.Info("Valkey connection established")

	accessTokenSigner, err := token.NewAccessTokenSigner(
		cfg.AccessTokenPrivateKeyPath,
		cfg.AccessTokenIssuer,
		cfg.AccessTokenAudience,
		cfg.AccessTokenKeyID,
		durations.AccessTokenTTL,
	)
	if err != nil {
		logger.Error(
			"failed to configure access token signer",
			"error", err,
		)
		return 1
	}

	systemClock := clockinfra.NewSystemClock()

	sessionStore := token.NewSessionStore(
		databasePool,
	)

	refreshTokenGenerator :=
		token.NewRefreshTokenGenerator()

	refreshTokenHasher :=
		token.NewRefreshTokenHasher()

	refreshTokenRotationStore :=
		postgresrepo.NewRefreshTokenRotationStore(
			databasePool,
		)

	sessionRevocationStore :=
		postgresrepo.NewSessionRevocationStore(
			databasePool,
		)

	sessionAccessRevocationStore :=
		valkeyrepo.NewSessionAccessRevocationStore(
			valkeyClient,
		)

	coordinatedSessionRevocationStore, err :=
		auth.NewCoordinatedSessionRevocationStore(
			sessionRevocationStore,
			sessionAccessRevocationStore,
			sessionRevocationStore,
		)
	if err != nil {
		logger.Error(
			"failed to configure coordinated session revocation",
			"error", err,
		)
		return 1
	}

	allSessionsRevocationStore :=
		postgresrepo.NewAllSessionsRevocationStore(
			databasePool,
		)

	coordinatedAllSessionsRevocationStore, err :=
		auth.NewCoordinatedAllSessionsRevocationStore(
			allSessionsRevocationStore,
			sessionAccessRevocationStore,
			allSessionsRevocationStore,
		)
	if err != nil {
		logger.Error(
			"failed to configure coordinated all sessions revocation",
			"error", err,
		)
		return 1
	}

	cleanupStore :=
		postgresrepo.NewCleanupStore(
			databasePool,
		)

	cleanupRunner := cleanupinfra.NewRunner(
		cleanupStore,
		logger,
		durations.CleanupInterval,
		durations.OTPRequestEventRetention,
		durations.OTPChallengeRetention,
		durations.AuthSessionRetention,
	)

	tokenIssuer, err := token.NewIssuer(
		token.NewSessionIDGenerator(),
		refreshTokenGenerator,
		accessTokenSigner,
		sessionStore,
		systemClock,
		durations.SessionTTL,
		durations.RefreshTokenTTL,
	)
	if err != nil {
		logger.Error(
			"failed to configure token issuer",
			"error", err,
		)
		return 1
	}

	challengeRepository :=
		postgresrepo.NewChallengeRepository(
			databasePool,
		)

	identityRepository :=
		postgresrepo.NewIdentityRepository(
			databasePool,
		)

	otpRateLimiter :=
		postgresrepo.NewOTPRequestRateLimiter(
			databasePool,
		)

	authService := auth.NewService(
		challengeRepository,
		identityRepository,
		otp.NewGenerator(),
		otpHasher,
		otpDelivery,
		otpRateLimiter,
		identifier.NewChallengeIDGenerator(),
		tokenIssuer,
		refreshTokenRotationStore,
		coordinatedSessionRevocationStore,
		coordinatedAllSessionsRevocationStore,
		refreshTokenGenerator,
		refreshTokenHasher,
		accessTokenSigner,
		systemClock,
		durations.OTPChallengeTTL,
		auth.OTPRequestRateLimitPolicy{
			Cooldown:    otpRequestRateLimit.Cooldown,
			Window:      otpRequestRateLimit.Window,
			MaxRequests: otpRequestRateLimit.MaxRequests,
		},
		durations.RefreshTokenTTL,
	)

	server := grpcserver.NewServer(
		cfg.GRPCAddress,
		logger,
	)

	identityHandler := grpcserver.NewIdentityHandler(
		authService,
	)

	server.RegisterIdentityService(
		identityHandler,
	)

	logger.Info(
		"identity service starting",
		"grpc_address", cfg.GRPCAddress,
	)

	shutdownContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	cleanupDone := make(chan struct{})

	go func() {
		defer close(cleanupDone)

		cleanupRunner.Run(
			shutdownContext,
		)
	}()

	serverError := make(chan error, 1)

	go func() {
		serverError <- server.Run()
	}()

	select {
	case err := <-serverError:
		stopSignals()

		<-cleanupDone

		if err != nil {
			logger.Error(
				"identity service stopped with error",
				"error", err,
			)
			return 1
		}

		return 0

	case <-shutdownContext.Done():
		logger.Info(
			"shutdown signal received",
		)
	}

	gracefulShutdownDone := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(gracefulShutdownDone)
	}()

	const gracefulShutdownTimeout = 10 * time.Second

	select {
	case <-gracefulShutdownDone:
		logger.Info(
			"identity service stopped gracefully",
		)

	case <-time.After(gracefulShutdownTimeout):
		logger.Warn(
			"graceful shutdown timed out; forcing gRPC server stop",
			"timeout", gracefulShutdownTimeout,
		)

		server.Stop()

		<-gracefulShutdownDone
	}

	<-cleanupDone

	return 0
}
