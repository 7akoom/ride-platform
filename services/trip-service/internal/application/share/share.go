// Package share is the rider's trip sharing: a link a rider sends to people
// they trust, who can follow the trip without an account while it is under
// way and for a while after it ends.
package share

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var (
	// ErrNotFound: no live link has this token (unknown, stopped, expired,
	// or its trip ended long enough ago). One answer for all of them, so a
	// token tells nothing about why it does not work.
	ErrNotFound = errors.New("this link has expired or does not exist")

	// ErrTripNotLive: only a trip under way is shared.
	ErrTripNotLive = errors.New("only a trip that is under way can be shared")

	// ErrTooManyLinks: a trip has at most MaxLiveLinks live links.
	ErrTooManyLinks = errors.New("this trip is shared too many times already; stop sharing it to start again")
)

// MaxLiveLinks is how many live links a trip may have at once.
const MaxLiveLinks = 5

// tokenBytes is a token's randomness: 256 bits, 43 characters in base64url.
const tokenBytes = 32

// Link is one share of a trip. The store keeps only its token's hash.
type Link struct {
	ID        string
	TripID    string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Created is a link as its rider gets it, the only time its token is seen.
type Created struct {
	Token     string
	URL       string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store keeps links.
type Store interface {
	// Create adds a link unless its trip has maxLive live links (not
	// revoked, not expired at link.CreatedAt): ErrTooManyLinks.
	Create(ctx context.Context, link Link, maxLive int) (Link, error)
	// FindLive returns the link with this token hash that is neither
	// revoked nor expired at now, or ErrNotFound.
	FindLive(ctx context.Context, tokenHash []byte, now time.Time) (Link, error)
	// RevokeAll stops every live link of the trip and says how many.
	RevokeAll(ctx context.Context, tripID string, now time.Time) (int, error)
}

// Trips reads trips.
type Trips interface {
	GetTrip(ctx context.Context, tripID string) (trip.Trip, error)
}

// Settings are the deployment's rules (config.Share).
type Settings struct {
	URLBase  string
	MaxAge   time.Duration
	AfterEnd time.Duration
}

// View is what a link shows: where the trip goes, how far along it is, who
// drives it in which car, and where they are. Never who the rider is, what
// they pay, or any phone number.
type View struct {
	Trip trip.Trip
	// Driver is nil before a driver accepts, or when their profile could not
	// be read.
	Driver *trip.DriverSummary
	// Location is the driver's last position while the trip is accepted or
	// in progress, when known.
	Location *trip.DriverLocation
	// ExpiresAt is when the link stops working.
	ExpiresAt time.Time
}

type Service struct {
	store    Store
	trips    Trips
	drivers  trip.DriverDirectory
	locator  trip.DriverLocator
	settings Settings
	now      func() time.Time
	random   func([]byte) (int, error)
}

func NewService(store Store, trips Trips, drivers trip.DriverDirectory, locator trip.DriverLocator, settings Settings) *Service {
	if store == nil || trips == nil || drivers == nil || locator == nil {
		panic("share store, trips, driver directory and locator are required")
	}

	return &Service{
		store: store, trips: trips, drivers: drivers, locator: locator, settings: settings,
		now: time.Now, random: rand.Read,
	}
}

func live(status trip.Status) bool {
	switch status {
	case trip.StatusRequested, trip.StatusAccepted, trip.StatusInProgress:
		return true
	default:
		return false
	}
}

// Create makes a new link to a trip under way. The caller has checked that
// the trip is the rider's.
func (s *Service) Create(ctx context.Context, tripID string) (Created, error) {
	found, err := s.trips.GetTrip(ctx, strings.TrimSpace(tripID))
	if err != nil {
		return Created{}, err
	}

	if !live(found.Status) {
		return Created{}, ErrTripNotLive
	}

	raw := make([]byte, tokenBytes)
	if _, err := s.random(raw); err != nil {
		return Created{}, fmt.Errorf("make a link token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now().UTC()

	link, err := s.store.Create(ctx, Link{
		TripID:    found.ID,
		TokenHash: hashOf(token),
		CreatedAt: now,
		ExpiresAt: now.Add(s.settings.MaxAge),
	}, MaxLiveLinks)
	if err != nil {
		return Created{}, err
	}

	out := Created{Token: token, CreatedAt: link.CreatedAt, ExpiresAt: link.ExpiresAt}
	if s.settings.URLBase != "" {
		out.URL = s.settings.URLBase + token
	}

	return out, nil
}

// Stop ends every live link to the trip.
func (s *Service) Stop(ctx context.Context, tripID string) (int, error) {
	found, err := s.trips.GetTrip(ctx, strings.TrimSpace(tripID))
	if err != nil {
		return 0, err
	}

	return s.store.RevokeAll(ctx, found.ID, s.now().UTC())
}

// View is what the link with this token shows, or ErrNotFound.
func (s *Service) View(ctx context.Context, token string) (View, error) {
	if !wellFormed(token) {
		return View{}, ErrNotFound
	}

	now := s.now().UTC()

	link, err := s.store.FindLive(ctx, hashOf(token), now)
	if err != nil {
		return View{}, err
	}

	found, err := s.trips.GetTrip(ctx, link.TripID)
	if errors.Is(err, trip.ErrTripNotFound) {
		return View{}, ErrNotFound
	}

	if err != nil {
		return View{}, err
	}

	view := View{Trip: found, ExpiresAt: link.ExpiresAt}

	if ended := endedAt(found); ended != nil {
		closes := ended.Add(s.settings.AfterEnd)
		if !now.Before(closes) {
			return View{}, ErrNotFound
		}

		if closes.Before(view.ExpiresAt) {
			view.ExpiresAt = closes
		}
	}

	// Who drives and where they are are extras: a link still shows the trip
	// when they cannot be read.
	if found.DriverID != "" {
		if summary, err := s.drivers.DriverSummary(ctx, found.DriverID); err == nil {
			view.Driver = &summary
		}

		if found.Status == trip.StatusAccepted || found.Status == trip.StatusInProgress {
			if position, err := s.locator.DriverLocation(ctx, found.DriverID); err == nil {
				view.Location = &position
			}
		}
	}

	return view, nil
}

func endedAt(t trip.Trip) *time.Time {
	switch t.Status {
	case trip.StatusCompleted:
		return t.CompletedAt
	case trip.StatusCancelled:
		return t.CancelledAt
	default:
		return nil
	}
}

func hashOf(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

// wellFormed: 43 base64url characters, the only shape a token has.
func wellFormed(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}

	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}

	return true
}
