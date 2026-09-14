package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
	"github.com/7akoom/ride-platform/services/notification-service/internal/config"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/channels"
	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/database"
	postgresrepo "github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/persistence/postgres"
	grpcserver "github.com/7akoom/ride-platform/services/notification-service/internal/transport/grpc"
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

	pushSender := buildPushSender(cfg, logger)

	notificationService := notification.NewService(
		postgresrepo.NewNotificationRepository(pool),
		pushSender,
		// SMS gateways are regional; wire a local provider here when
		// the deployment has one.
		channels.NewNoopSMSSender(),
	)
	notificationHandler := grpcserver.NewNotificationHandler(notificationService)

	server := grpcserver.NewServer(cfg.GRPCAddress, logger)
	server.RegisterNotificationService(notificationHandler)

	// TODO: subscribe to trip.*, fare.calculated and trip.settled events
	// so notifications fire automatically instead of being sent
	// explicitly. Part of the same deferred NATS pass as every service.

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Run()
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
