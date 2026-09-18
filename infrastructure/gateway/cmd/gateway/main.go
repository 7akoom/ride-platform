package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/infrastructure/gateway/internal/config"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	mux := runtime.NewServeMux()

	tripConn, err := dialBackend(cfg.TripServiceAddress)
	if err != nil {
		logger.Error("failed to connect to trip-service", "error", err)

		return 1
	}
	defer tripConn.Close()

	if err := tripv1.RegisterTripServiceHandler(ctx, mux, tripConn); err != nil {
		logger.Error("failed to register trip-service gateway handler", "error", err)

		return 1
	}

	walletConn, err := dialBackend(cfg.WalletServiceAddress)
	if err != nil {
		logger.Error("failed to connect to wallet-service", "error", err)

		return 1
	}
	defer walletConn.Close()

	if err := walletv1.RegisterWalletServiceHandler(ctx, mux, walletConn); err != nil {
		logger.Error("failed to register wallet-service gateway handler", "error", err)

		return 1
	}

	locationConn, err := dialBackend(cfg.LocationServiceAddress)
	if err != nil {
		logger.Error("failed to connect to location-service", "error", err)

		return 1
	}
	defer locationConn.Close()

	if err := locationv1.RegisterLocationServiceHandler(ctx, mux, locationConn); err != nil {
		logger.Error("failed to register location-service gateway handler", "error", err)

		return 1
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           withCORS(cfg.AllowedOrigins, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info("gateway listening", "address", cfg.HTTPAddress)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err

			return
		}

		serverErrors <- nil
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("gateway server exited with error", "error", err)

			return 1
		}

	case <-ctx.Done():
		logger.Info("shutdown signal received, stopping gateway server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Warn("failed to shut down gateway server cleanly", "error", err)
		}
	}

	return 0
}

// dialBackend connects to a backend service on the END USER's behalf, not
// the gateway's own. Unlike a service-to-service client (see e.g.
// dispatch-service's dialService), it does NOT inject the internal
// service token: the caller's own Authorization header is forwarded
// automatically by grpc-gateway's runtime.AnnotateContext (Authorization
// is one of its permanent headers), so the backend's existing
// authentication interceptor sees the real end-user access token.
func dialBackend(address string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// withCORS allows the Admin web app (a browser) to call this gateway
// from a different origin. Mobile apps don't send an Origin header and
// pass through untouched either way.
func withCORS(allowedOrigins string, next http.Handler) http.Handler {
	origins := map[string]bool{}

	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)

			return
		}

		next.ServeHTTP(w, r)
	})
}
