// Package tips is a rider thanking the driver of a completed trip with money
// from their wallet: once per trip, soon after it, all of it to the driver.
package tips

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// Window is how long after a trip is settled its rider may tip.
const Window = 72 * time.Hour

const maxKeyLength = 120

var (
	ErrTripRequired   = errors.New("rider_id and trip_id are required")
	ErrInvalidAmount  = errors.New("amount must be a positive decimal with at most 3 decimal places")
	ErrIdempotencyKey = errors.New("idempotency_key is required (1-120 characters)")
	ErrBelowMin       = errors.New("the tip is below the least a tip may be")
	ErrAboveMax       = errors.New("the tip is above the most a tip may be")
	ErrKeyReused      = errors.New("this idempotency_key was already used for another tip")
	// ErrTripNotFound: no completed trip of this rider's has this id.
	ErrTripNotFound = errors.New("no completed trip of yours has this id")
	// ErrNotTippable: a cancelled trip's fee, or too long ago.
	ErrNotTippable   = errors.New("this trip can no longer be tipped (only a completed trip, within 72 hours)")
	ErrAlreadyTipped = errors.New("this trip was tipped already")
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Tip is money a rider gave a trip's driver.
type Tip struct {
	ID             string
	TripID         string
	RiderID        string
	DriverID       string
	CurrencyCode   string
	Amount         wallet.Money
	IdempotencyKey string
	CreatedAt      time.Time
}

// Record is a tip to write.
type Record struct {
	TripID         string
	RiderID        string
	Amount         wallet.Money
	IdempotencyKey string
}

// Store moves a tip with its ledger rows and the wallet.tip_received event.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)
	FindByKey(ctx context.Context, riderID, key string) (Tip, bool, error)
	// Tip checks the trip's settlement (this rider's, a completed trip,
	// settled after notBefore), then moves the amount from the rider's
	// wallet to the driver's in one transaction. ErrTripNotFound,
	// ErrNotTippable, ErrAlreadyTipped, wallet.ErrInsufficientFunds,
	// wallet.ErrDuplicateRequest (the key was taken meanwhile).
	Tip(ctx context.Context, record Record, notBefore time.Time) (Tip, wallet.Wallet, error)
}

// Service gives tips.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	if store == nil {
		panic("tip store is required")
	}

	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Input is a rider tipping a trip's driver.
type Input struct {
	RiderID        string
	TripID         string
	Amount         wallet.Money
	IdempotencyKey string
}

// Give moves the tip. A retry with the same key returns the tip given the
// first time (with no wallet: the caller reads it as it is now).
func (s *Service) Give(ctx context.Context, input Input) (Tip, wallet.Wallet, error) {
	rider := strings.TrimSpace(input.RiderID)
	trip := strings.TrimSpace(input.TripID)
	key := strings.TrimSpace(input.IdempotencyKey)

	switch {
	case !uuidPattern.MatchString(rider) || !uuidPattern.MatchString(trip):
		return Tip{}, wallet.Wallet{}, ErrTripRequired
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(3)):
		return Tip{}, wallet.Wallet{}, ErrInvalidAmount
	case key == "" || len(key) > maxKeyLength:
		return Tip{}, wallet.Wallet{}, ErrIdempotencyKey
	}

	replay := func() (Tip, bool, error) {
		done, found, err := s.store.FindByKey(ctx, rider, key)
		if err != nil {
			return Tip{}, false, fmt.Errorf("look up the idempotency key: %w", err)
		}

		if found && (done.TripID != trip || !done.Amount.Equal(input.Amount)) {
			return Tip{}, true, ErrKeyReused
		}

		return done, found, nil
	}

	if done, found, err := replay(); err != nil || found {
		return done, wallet.Wallet{}, err
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Tip{}, wallet.Wallet{}, fmt.Errorf("read the wallet config: %w", err)
	}

	switch {
	case input.Amount.LessThan(config.TipMinAmount):
		return Tip{}, wallet.Wallet{}, ErrBelowMin
	case input.Amount.GreaterThan(config.TipMaxAmount):
		return Tip{}, wallet.Wallet{}, ErrAboveMax
	}

	given, balance, err := s.store.Tip(ctx, Record{TripID: trip, RiderID: rider, Amount: input.Amount, IdempotencyKey: key}, s.now().Add(-Window))
	if errors.Is(err, wallet.ErrDuplicateRequest) {
		if done, found, findErr := replay(); findErr != nil || found {
			return done, wallet.Wallet{}, findErr
		}
	}

	return given, balance, err
}
