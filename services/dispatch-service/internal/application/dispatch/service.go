package dispatch

import (
	"context"
	"io"
	"log/slog"
	"time"
)

type Service interface {
	DispatchTrip(
		ctx context.Context,
		tripID string,
		searchRadiusMeters float64,
	) (Result, error)
}

// discardLogger is what a service without WithLogger uses, so callers and
// tests that build a service directly never hit a nil logger.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type service struct {
	tripClient     TripClient
	locationClient LocationClient
	driverClient   DriverClient
	walletClient   WalletClient
	logger         *slog.Logger

	// offerTTL, when positive, makes dispatch offer a trip to a driver for this long
	// instead of assigning it (see WithOffers).
	offerTTL time.Duration
}

// Option customises a service at construction time.
type Option func(*service)

// WithLogger makes the service explain, at Info level, why a dispatch
// attempt found nobody to assign. Without it the service stays silent,
// which is what unit tests want.
func WithLogger(logger *slog.Logger) Option {
	return func(s *service) {
		if logger != nil {
			s.logger = logger
		}
	}
}

func NewService(
	tripClient TripClient,
	locationClient LocationClient,
	driverClient DriverClient,
	walletClient WalletClient,
	options ...Option,
) Service {
	if tripClient == nil {
		panic("trip client is required")
	}

	if locationClient == nil {
		panic("location client is required")
	}

	if driverClient == nil {
		panic("driver client is required")
	}

	if walletClient == nil {
		panic("wallet client is required")
	}

	s := &service{
		tripClient:     tripClient,
		locationClient: locationClient,
		driverClient:   driverClient,
		walletClient:   walletClient,
		logger:         discardLogger,
	}

	for _, option := range options {
		option(s)
	}

	return s
}

// log never returns nil.
func (s *service) log() *slog.Logger {
	if s.logger == nil {
		return discardLogger
	}

	return s.logger
}
