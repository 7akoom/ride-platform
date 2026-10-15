package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var (
	// ErrLookupLimit: the rider looked up as many phones as allowed for now.
	ErrLookupLimit = errors.New("too many phone lookups; try again later")
	// ErrLookupsUnavailable: the service was built without recipient lookups.
	ErrLookupsUnavailable = errors.New("recipient lookups are not available")
)

// Preflight is what the app reads before a transfer: how much has gone out
// in the last 24 hours (the daily limits count those) and the lookups made.
type Preflight interface {
	// SentLastDay is the rider's transfers sent in the last 24 hours, as the
	// send checks them.
	SentLastDay(ctx context.Context, riderID string) (wallet.Money, int, error)
	// Lookups counts the rider's recipient lookups after since.
	Lookups(ctx context.Context, riderID string, since time.Time) (int, error)
	// RecordLookup counts one lookup and forgets the rider's lookups before
	// forgetBefore.
	RecordLookup(ctx context.Context, riderID string, at, forgetBefore time.Time) error
}

// Directory finds a person's rider profile with the name it shows.
type Directory interface {
	RiderByIdentity(ctx context.Context, identityID string) (id, displayName string, err error)
}

// LookupLimits is how many phones a rider may look up in an hour and a day.
type LookupLimits struct {
	PerHour int
	PerDay  int
}

// WithLookups gives the service transfer options and recipient lookups.
func (s *Service) WithLookups(preflight Preflight, directory Directory, limits LookupLimits) *Service {
	if preflight == nil || directory == nil {
		panic("transfer preflight store and rider directory are required")
	}

	if limits.PerHour < 1 || limits.PerDay < limits.PerHour {
		panic("recipient lookup limits must be at least 1 an hour and at least as many a day")
	}

	s.preflight = preflight
	s.directory = directory
	s.lookupLimits = limits

	return s
}

// Options is what a transfer may be for this rider now.
type Options struct {
	CurrencyCode string
	MinAmount    wallet.Money
	MaxAmount    wallet.Money
	DailyAmount  wallet.Money
	DailyCount   int
	SentAmount   wallet.Money
	SentCount    int
}

// Options reads the transfer limits and what the rider sent in the last 24
// hours.
func (s *Service) Options(ctx context.Context, riderID string) (Options, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return Options{}, ErrSenderRequired
	}

	if s.preflight == nil {
		return Options{}, ErrLookupsUnavailable
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Options{}, fmt.Errorf("read the wallet config: %w", err)
	}

	sentAmount, sentCount, err := s.preflight.SentLastDay(ctx, riderID)
	if err != nil {
		return Options{}, fmt.Errorf("sum the last day's transfers: %w", err)
	}

	return Options{
		CurrencyCode: config.CurrencyCode,
		MinAmount:    config.TransferMinAmount,
		MaxAmount:    config.TransferMaxAmount,
		DailyAmount:  config.TransferDailyAmount,
		DailyCount:   config.TransferDailyCount,
		SentAmount:   sentAmount,
		SentCount:    sentCount,
	}, nil
}

// LookupResult is what a phone is for a transfer.
type LookupResult string

const (
	// RecipientFound: a rider who can receive a transfer.
	RecipientFound LookupResult = "found"
	// RecipientNotFound: nobody a transfer can reach (not registered, not
	// active, or no rider profile).
	RecipientNotFound LookupResult = "not_found"
	// RecipientSelf: the rider's own phone.
	RecipientSelf LookupResult = "self"
)

// Recipient is a looked-up phone: the name is shortened ("Haidar K.").
type Recipient struct {
	Result      LookupResult
	DisplayName string
}

// LookupInput is a rider checking a phone before sending to it.
type LookupInput struct {
	RiderID    string
	IdentityID string
	Phone      string
}

// Lookup says whether a transfer to the phone would reach a rider, with the
// rider's shortened name. It finds exactly whom a send would (an active
// identity with a rider profile). Lookups are counted per rider, so the
// phones of riders cannot be listed by trying numbers.
func (s *Service) Lookup(ctx context.Context, input LookupInput) (Recipient, error) {
	riderID := strings.TrimSpace(input.RiderID)
	identityID := strings.TrimSpace(input.IdentityID)
	phone := strings.TrimSpace(input.Phone)

	switch {
	case riderID == "":
		return Recipient{}, ErrSenderRequired
	case identityID == "":
		return Recipient{}, ErrIdentityRequired
	case !phonePattern.MatchString(phone):
		return Recipient{}, ErrInvalidPhone
	case s.preflight == nil:
		return Recipient{}, ErrLookupsUnavailable
	}

	now := s.now()

	if err := s.checkLookupLimits(ctx, riderID, now); err != nil {
		return Recipient{}, err
	}

	if err := s.preflight.RecordLookup(ctx, riderID, now, now.Add(-24*time.Hour)); err != nil {
		return Recipient{}, fmt.Errorf("count the lookup: %w", err)
	}

	recipientIdentity, found, err := s.identity.FindByPhone(ctx, phone)
	if err != nil {
		return Recipient{}, err
	}

	if !found {
		return Recipient{Result: RecipientNotFound}, nil
	}

	if recipientIdentity == identityID {
		return Recipient{Result: RecipientSelf}, nil
	}

	recipientRider, name, err := s.directory.RiderByIdentity(ctx, recipientIdentity)
	if err != nil {
		return Recipient{}, fmt.Errorf("find the recipient's rider profile: %w", err)
	}

	switch recipientRider {
	case "":
		return Recipient{Result: RecipientNotFound}, nil
	case riderID:
		return Recipient{Result: RecipientSelf}, nil
	}

	return Recipient{Result: RecipientFound, DisplayName: ShortName(name)}, nil
}

func (s *Service) checkLookupLimits(ctx context.Context, riderID string, now time.Time) error {
	lastHour, err := s.preflight.Lookups(ctx, riderID, now.Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count the last hour's lookups: %w", err)
	}

	if lastHour >= s.lookupLimits.PerHour {
		return ErrLookupLimit
	}

	lastDay, err := s.preflight.Lookups(ctx, riderID, now.Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("count the last day's lookups: %w", err)
	}

	if lastDay >= s.lookupLimits.PerDay {
		return ErrLookupLimit
	}

	return nil
}

// ShortName is the first name and the initial of the last one: "Haidar
// Kamal Ali" is "Haidar A.". A single name stays as it is.
func ShortName(name string) string {
	parts := strings.Fields(name)

	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}

	initial, _ := utf8.DecodeRuneInString(parts[len(parts)-1])

	return parts[0] + " " + string(initial) + "."
}
