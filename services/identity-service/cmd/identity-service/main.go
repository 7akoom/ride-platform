package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	cleanupinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/cleanup"
	clockinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/identifier"
	natsinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/messaging/nats"
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

	metricsRuntime, err := observability.NewMetricsRuntime(
		cfg.ServiceName,
		cfg.MetricsAddress,
	)
	if err != nil {
		logger.Error(
			"failed to configure metrics runtime",
			"error", err,
		)

		return 1
	}

	authMetrics, err := metricsRuntime.NewAuthMetrics()
	if err != nil {
		logger.Error(
			"failed to configure authentication metrics",
			"error", err,
		)

		return 1
	}

	authMetricsRecorder, err :=
		observability.NewAuthMetricsRecorder(
			authMetrics,
		)
	if err != nil {
		logger.Error(
			"failed to configure authentication metrics recorder",
			"error", err,
		)

		return 1
	}

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

	natsConfig, err := config.ParseNATS(cfg)
	if err != nil {
		logger.Error(
			"invalid NATS configuration",
			"error", err,
		)

		return 1
	}

	outboxConfig, err := config.ParseOutbox(cfg)
	if err != nil {
		logger.Error(
			"invalid outbox configuration",
			"error", err,
		)

		return 1
	}

	if err := config.ValidateRuntime(
		natsConfig,
		outboxConfig,
	); err != nil {
		logger.Error(
			"invalid runtime configuration",
			"error", err,
		)

		return 1
	}

	if err := config.ValidateProductionProviders(
		cfg,
	); err != nil {
		logger.Error(
			"invalid production provider configuration",
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

	otpDeliveryTrackingStore :=
		postgresrepo.NewOTPDeliveryTrackingStore(
			databasePool,
		)

	var otpDelivery auth.OTPDelivery

	environment := strings.ToLower(
		strings.TrimSpace(
			cfg.Environment,
		),
	)

	switch environment {
	case "development", "test":
		otpDelivery, err =
			otp.NewDevelopmentDelivery(
				environment,
				logger,
				otp.WithDevelopmentDeliveryMetricsRecorder(
					authMetricsRecorder,
				),
			)

	case "production":
		otpDelivery, err =
			buildProductionOTPDeliveryWithTracking(
				cfg,
				otpDeliveryTrackingStore,
				authMetricsRecorder,
			)

	default:
		err = fmt.Errorf(
			"unsupported APP_ENV %q",
			cfg.Environment,
		)
	}

	if err != nil {
		logger.Error(
			"failed to configure OTP delivery",
			"error", err,
		)

		return 1
	}

	otpWebhookServer, err :=
		buildOTPWebhookServer(
			cfg,
			otpDeliveryTrackingStore,
			authMetricsRecorder,
		)
	if err != nil {
		logger.Error(
			"failed to configure OTP webhook server",
			"error", err,
		)

		return 1
	}

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
		logger.Error(
			"failed to configure NATS connection",
			"error", err,
		)

		return 1
	}

	defer func() {
		if err := natsConnection.Drain(); err != nil {
			logger.Warn(
				"failed to drain NATS connection",
				"error", err,
			)
		}
	}()

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

	outboxStore :=
		postgresrepo.NewOutboxStore(
			databasePool,
		)

	if err := metricsRuntime.RegisterOutboxMetrics(outboxStore, logger); err != nil {
		logger.Error("failed to configure outbox metrics", "error", err)
		return 1
	}

	outboxPublisher :=
		natsinfra.NewJetStreamPublisher(
			natsConnection.JetStream(),
			natsConfig.PublishTimeout,
		)

	outboxProcessor :=
		outboxapp.NewProcessor(
			outboxStore,
			outboxPublisher,
			systemClock,
			outboxapp.ProcessorConfig{
				BatchSize: outboxConfig.BatchSize,

				LeaseDuration: outboxConfig.LeaseDuration,

				InitialRetryDelay: outboxConfig.InitialRetryDelay,

				MaxRetryDelay: outboxConfig.MaxRetryDelay,
			},
		)

	outboxWorker :=
		outboxapp.NewWorker(
			outboxProcessor,
			logger,
			outboxapp.WorkerConfig{
				PollInterval: outboxConfig.PollInterval,
			},
		)

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

	sessionReader :=
		postgresrepo.NewSessionReader(
			databasePool,
		)

	sessionAccessRevocationStore :=
		valkeyrepo.NewSessionAccessRevocationStore(
			valkeyClient,
		)

	sessionAccessChecker, err :=
		token.NewSessionAccessChecker(
			sessionAccessRevocationStore,
			sessionRevocationStore,
			systemClock,
		)
	if err != nil {
		logger.Error(
			"failed to configure session access checker",
			"error", err,
		)

		return 1
	}

	accessTokenVerificationKeys, err :=
		parseAccessTokenVerificationKeys(
			cfg.AccessTokenVerificationKeys,
			cfg.AccessTokenPublicKeyPath,
			cfg.AccessTokenKeyID,
		)
	if err != nil {
		logger.Error(
			"failed to configure access token verification keyring",
			"error", err,
		)

		return 1
	}

	accessTokenVerifier, err :=
		token.NewAccessTokenVerifierWithKeyring(
			accessTokenVerificationKeys,
			cfg.AccessTokenIssuer,
			cfg.AccessTokenAudience,
			sessionAccessChecker,
			token.WithAccessTokenVerificationMetricsRecorder(
				authMetricsRecorder,
			),
		)

	if err != nil {
		logger.Error(
			"failed to configure access token verifier",
			"error", err,
		)

		return 1
	}

	if err := accessTokenVerifier.ValidateSigner(accessTokenSigner); err != nil {
		logger.Error("inconsistent access token signing configuration", "error", err)
		return 1
	}

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

	coordinatedSessionManagementRevocationStore, err :=
		auth.NewCoordinatedSessionManagementRevocationStore(
			sessionRevocationStore,
			sessionAccessRevocationStore,
			allSessionsRevocationStore,
		)
	if err != nil {
		logger.Error(
			"failed to configure coordinated session management revocation",
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

	identityIdentifierRepository :=
		postgresrepo.NewIdentityIdentifierRepository(
			databasePool,
		)

	identityReader :=
		postgresrepo.NewIdentityReader(
			databasePool,
		)

	identityLifecycleStore :=
		postgresrepo.NewIdentityLifecycleStore(
			databasePool,
		)

	identifierLinkCompletionStore :=
		postgresrepo.NewIdentifierLinkCompletionStore(
			databasePool,
		)

	identifierUnlinkRequestStore :=
		postgresrepo.NewIdentifierUnlinkRequestStore(
			databasePool,
		)

	identifierUnlinkCompletionStore :=
		postgresrepo.NewIdentifierUnlinkCompletionStore(
			databasePool,
		)

	otpRateLimiter :=
		postgresrepo.NewOTPRequestRateLimiter(
			databasePool,
		)

	authService := auth.NewServiceWithIdentityLifecycle(
		challengeRepository,
		identityIdentifierRepository,
		identityReader,
		identityLifecycleStore,
		identifierLinkCompletionStore,
		identifierUnlinkRequestStore,
		identifierUnlinkCompletionStore,
		otp.NewGenerator(),
		otpHasher,
		otpDelivery,
		otpRateLimiter,
		identifier.NewChallengeIDGenerator(),
		tokenIssuer,
		refreshTokenRotationStore,
		coordinatedSessionRevocationStore,
		coordinatedAllSessionsRevocationStore,
		sessionReader,
		coordinatedSessionManagementRevocationStore,
		refreshTokenGenerator,
		refreshTokenHasher,
		accessTokenSigner,
		systemClock,
		durations.OTPChallengeTTL,
		auth.OTPRequestRateLimitPolicy{
			Cooldown:    otpRequestRateLimit.Cooldown,
			Window:      otpRequestRateLimit.Window,
			MaxRequests: otpRequestRateLimit.MaxRequests,
			Abuse: auth.OTPRequestAbuseLimitPolicy{
				Window:      otpRequestRateLimit.SourceWindow,
				MaxRequests: otpRequestRateLimit.SourceMaxRequests,
			},
		},
		durations.RefreshTokenTTL,
		auth.WithMetricsRecorder(
			authMetricsRecorder,
		),
	)

	server := grpcserver.NewServer(
		cfg.GRPCAddress,
		logger,
		grpcserver.NewRequestSourceUnaryInterceptor(),
		grpcserver.NewAuthenticationUnaryInterceptor(
			func(
				ctx context.Context,
				rawToken string,
			) (string, string, string, error) {
				claims, err := accessTokenVerifier.Verify(
					ctx,
					rawToken,
				)
				if err != nil {
					return "", "", "", err
				}

				return claims.Subject,
					claims.SessionID,
					claims.TenantHint,
					nil
			},
		),
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
		"metrics_address", cfg.MetricsAddress,
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

	outboxDone := make(chan struct{})

	go func() {
		defer close(outboxDone)

		if err := outboxWorker.Run(
			shutdownContext,
		); err != nil {
			logger.Error(
				"outbox worker stopped with error",
				"error", err,
			)
		}
	}()

	serverError := make(
		chan error,
		1,
	)

	go func() {
		serverError <- server.Run()
	}()

	metricsError := make(
		chan error,
		1,
	)

	go func() {
		metricsError <- metricsRuntime.Serve()
	}()

	var otpWebhookError chan error

	if otpWebhookServer != nil {
		otpWebhookError = make(
			chan error,
			1,
		)

		go func() {
			otpWebhookError <- otpWebhookServer.Run()
		}()

		logger.Info(
			"OTP webhook server started",
			"address", cfg.OTPWebhookAddress,
		)
	}

	exitCode := 0

	select {
	case err := <-serverError:
		stopSignals()

		if err != nil {
			logger.Error(
				"identity service stopped with error",
				"error", err,
			)

			exitCode = 1
		} else {
			logger.Info(
				"gRPC server stopped",
			)
		}

	case err := <-metricsError:
		stopSignals()

		if err != nil {
			logger.Error(
				"metrics server stopped with error",
				"error", err,
			)

			exitCode = 1
		} else {
			logger.Info(
				"metrics server stopped",
			)
		}

	case err := <-otpWebhookError:
		stopSignals()

		if err != nil {
			logger.Error(
				"OTP webhook server stopped with error",
				"error", err,
			)

			exitCode = 1
		} else {
			logger.Info(
				"OTP webhook server stopped",
			)
		}

	case <-shutdownContext.Done():
		logger.Info(
			"shutdown signal received",
		)
	}

	const gracefulShutdownTimeout = 10 * time.Second

	gracefulShutdownContext, cancelGracefulShutdown :=
		context.WithTimeout(
			context.Background(),
			gracefulShutdownTimeout,
		)
	defer cancelGracefulShutdown()

	grpcShutdownDone := make(
		chan struct{},
	)

	go func() {
		server.GracefulStop()
		close(grpcShutdownDone)
	}()

	metricsShutdownDone := make(
		chan error,
		1,
	)

	go func() {
		metricsShutdownDone <- metricsRuntime.Shutdown(
			gracefulShutdownContext,
		)
	}()

	var otpWebhookShutdownDone chan error

	otpWebhookStopped :=
		otpWebhookServer == nil

	if otpWebhookServer != nil {
		otpWebhookShutdownDone = make(
			chan error,
			1,
		)

		go func() {
			otpWebhookShutdownDone <- otpWebhookServer.Shutdown(
				gracefulShutdownContext,
			)
		}()
	}

	grpcStopped := false
	metricsStopped := false

	gracefulShutdownTimeoutDone :=
		gracefulShutdownContext.Done()

	for !grpcStopped ||
		!metricsStopped ||
		!otpWebhookStopped {
		select {
		case <-grpcShutdownDone:
			grpcStopped = true
			grpcShutdownDone = nil

		case err := <-metricsShutdownDone:
			metricsStopped = true
			metricsShutdownDone = nil

			if err != nil {
				logger.Error(
					"metrics runtime shutdown failed",
					"error", err,
				)
			}

		case err := <-otpWebhookShutdownDone:
			otpWebhookStopped = true
			otpWebhookShutdownDone = nil

			if err != nil {
				logger.Error(
					"OTP webhook server shutdown failed",
					"error", err,
				)
			}

		case <-gracefulShutdownTimeoutDone:
			logger.Warn(
				"graceful shutdown timed out; forcing shutdown",
				"timeout", gracefulShutdownTimeout,
			)

			gracefulShutdownTimeoutDone = nil

			if !grpcStopped {
				server.Stop()
			}

			if !otpWebhookStopped &&
				otpWebhookServer != nil {
				if err := otpWebhookServer.Close(); err != nil {
					logger.Error(
						"failed to force close OTP webhook server",
						"error", err,
					)
				}
			}
		}
	}

	if grpcStopped &&
		metricsStopped &&
		otpWebhookStopped {
		logger.Info(
			"identity service stopped gracefully",
		)
	}

	<-outboxDone
	<-cleanupDone

	return exitCode
}

type accessTokenVerificationKeyConfig struct {
	KeyID         string `json:"kid"`
	PublicKeyPath string `json:"public_key_path"`
}

func parseAccessTokenVerificationKeys(
	rawKeyring string,
	legacyPublicKeyPath string,
	activeKeyID string,
) ([]token.AccessTokenVerificationKey, error) {
	activeKeyID = strings.TrimSpace(
		activeKeyID,
	)

	if activeKeyID == "" {
		return nil, fmt.Errorf(
			"access token active key ID cannot be empty",
		)
	}

	rawKeyring = strings.TrimSpace(
		rawKeyring,
	)

	if rawKeyring == "" {
		legacyPublicKeyPath = strings.TrimSpace(
			legacyPublicKeyPath,
		)

		if legacyPublicKeyPath == "" {
			return nil, fmt.Errorf(
				"access token public key path cannot be empty when verification keyring is not configured",
			)
		}

		return []token.AccessTokenVerificationKey{
			{
				KeyID:         activeKeyID,
				PublicKeyPath: legacyPublicKeyPath,
			},
		}, nil
	}

	decoder := json.NewDecoder(
		strings.NewReader(rawKeyring),
	)

	decoder.DisallowUnknownFields()

	var configuredKeys []accessTokenVerificationKeyConfig

	if err := decoder.Decode(
		&configuredKeys,
	); err != nil {
		return nil, fmt.Errorf(
			"decode access token verification keyring: %w",
			err,
		)
	}

	if err := ensureJSONDocumentEnded(
		decoder,
	); err != nil {
		return nil, err
	}

	if len(configuredKeys) == 0 {
		return nil, fmt.Errorf(
			"access token verification keyring cannot be empty",
		)
	}

	keys := make(
		[]token.AccessTokenVerificationKey,
		0,
		len(configuredKeys),
	)

	seenKeyIDs := make(
		map[string]struct{},
		len(configuredKeys),
	)

	activeKeyFound := false

	for index, configuredKey := range configuredKeys {
		keyID := strings.TrimSpace(
			configuredKey.KeyID,
		)

		if keyID == "" {
			return nil, fmt.Errorf(
				"access token verification key at index %d has an empty key ID",
				index,
			)
		}

		publicKeyPath := strings.TrimSpace(
			configuredKey.PublicKeyPath,
		)

		if publicKeyPath == "" {
			return nil, fmt.Errorf(
				"access token verification key %q has an empty public key path",
				keyID,
			)
		}

		if _, exists := seenKeyIDs[keyID]; exists {
			return nil, fmt.Errorf(
				"duplicate access token verification key ID %q",
				keyID,
			)
		}

		seenKeyIDs[keyID] = struct{}{}

		if keyID == activeKeyID {
			activeKeyFound = true
		}

		keys = append(
			keys,
			token.AccessTokenVerificationKey{
				KeyID:         keyID,
				PublicKeyPath: publicKeyPath,
			},
		)
	}

	if !activeKeyFound {
		return nil, fmt.Errorf(
			"active access token key ID %q is not present in verification keyring",
			activeKeyID,
		)
	}

	return keys, nil
}

func ensureJSONDocumentEnded(
	decoder *json.Decoder,
) error {
	var trailing any

	err := decoder.Decode(
		&trailing,
	)

	if err == io.EOF {
		return nil
	}

	if err == nil {
		return fmt.Errorf(
			"access token verification keyring contains trailing JSON data",
		)
	}

	return fmt.Errorf(
		"decode trailing access token verification keyring data: %w",
		err,
	)
}
