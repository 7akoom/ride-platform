// Package dispatchsettings holds how trips are given out to drivers, which staff
// set from the admin panel and dispatch-service reads.
package dispatchsettings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultNextTripRadiusMeters: how near a new pickup must be to where a busy
	// driver drops their rider for the trip to be offered to them as a next trip.
	DefaultNextTripRadiusMeters = 3000
	MinNextTripRadiusMeters     = 500
	MaxNextTripRadiusMeters     = 10000

	// OfferSeconds bounds how long a driver has to accept an offer; 0 is the
	// instance's own default (DISPATCH_OFFER_TTL in dispatch-service).
	MinOfferSeconds = 5
	MaxOfferSeconds = 60
)

// ErrInvalidRadius: the radius is outside MinNextTripRadiusMeters..MaxNextTripRadiusMeters.
var ErrInvalidRadius = fmt.Errorf("next_trip_radius_meters must be between %d and %d", MinNextTripRadiusMeters, MaxNextTripRadiusMeters)

// ErrInvalidOfferSeconds: offer_seconds is neither 0 nor within MinOfferSeconds..MaxOfferSeconds.
var ErrInvalidOfferSeconds = fmt.Errorf("offer_seconds must be 0 (the instance's default) or between %d and %d", MinOfferSeconds, MaxOfferSeconds)

// ErrUnavailable: the service was built without a store.
var ErrUnavailable = errors.New("dispatch settings are not configured")

// Settings: NextTripsEnabled lets a driver whose rider is in the car be offered a
// trip whose pickup is within NextTripRadiusMeters of where they drop that rider.
// OfferSeconds is how long a driver has to accept an offer; 0 for the instance's
// default.
type Settings struct {
	NextTripsEnabled     bool
	NextTripRadiusMeters int
	OfferSeconds         int
	UpdatedBy            string
	UpdatedAt            time.Time
}

// Defaults are the settings of an instance staff never changed.
func Defaults() Settings {
	return Settings{NextTripsEnabled: true, NextTripRadiusMeters: DefaultNextTripRadiusMeters}
}

// Store keeps the one row of settings; Get returns Defaults when there is none.
type Store interface {
	GetDispatchSettings(ctx context.Context) (Settings, error)
	SetDispatchSettings(ctx context.Context, settings Settings) (Settings, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	if store == nil {
		panic("dispatch settings store is required")
	}

	return &Service{store: store}
}

func (s *Service) Get(ctx context.Context) (Settings, error) {
	return s.store.GetDispatchSettings(ctx)
}

// Update replaces the settings with these; UpdatedBy is the staff member's identity.
func (s *Service) Update(ctx context.Context, settings Settings) (Settings, error) {
	if settings.NextTripRadiusMeters < MinNextTripRadiusMeters || settings.NextTripRadiusMeters > MaxNextTripRadiusMeters {
		return Settings{}, ErrInvalidRadius
	}

	if settings.OfferSeconds != 0 && (settings.OfferSeconds < MinOfferSeconds || settings.OfferSeconds > MaxOfferSeconds) {
		return Settings{}, ErrInvalidOfferSeconds
	}

	settings.UpdatedBy = strings.TrimSpace(settings.UpdatedBy)
	settings.UpdatedAt = time.Time{}

	return s.store.SetDispatchSettings(ctx, settings)
}
