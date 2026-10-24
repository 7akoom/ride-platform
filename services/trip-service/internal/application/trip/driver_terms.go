package trip

import (
	"context"
	"errors"
	"time"
)

// ErrDriverTermsUnavailable: the trip's rate card terms cannot be read.
var ErrDriverTermsUnavailable = errors.New("the trip's terms are not available")

// RateTerms is what the rate card a trip was priced with says about waiting at the
// pickup and a rider who does not come (pricing-service). Money is a decimal string.
type RateTerms struct {
	CurrencyCode       string
	FreeWaitingMinutes int
	WaitingPerMinute   string
	NoShowWaitMinutes  int
	NoShowFee          string
}

// RateTermsReader reads a trip's rate card terms (pricing-service).
type RateTermsReader interface {
	RateTerms(ctx context.Context, tripID, riderID string) (RateTerms, error)
}

// DriverTerms is what the driver of a trip needs to wait at the pickup and act on time.
// FreeWaitingUntil and NoShowFrom are nil before the driver arrived.
type DriverTerms struct {
	RateTerms

	FreeWaitingUntil    *time.Time
	NoShowFrom          *time.Time
	ArrivalRadiusMeters int
}

// WithDriverTerms decorates a Service with GetDriverTripTerms. The transport layer
// reaches it through As.
func WithDriverTerms(base Service, terms RateTermsReader) Service {
	if base == nil {
		panic("trip service is required")
	}

	if terms == nil {
		panic("rate terms reader is required")
	}

	return &driverTermsService{Service: base, terms: terms}
}

type driverTermsService struct {
	Service

	terms RateTermsReader
}

// Unwrap returns the Service the decorator wraps, so As can keep looking through it.
func (s *driverTermsService) Unwrap() Service { return s.Service }

// GetDriverTripTerms reads the trip and its rate card terms. Who may ask (the driver of
// the trip) is decided before this is called.
func (s *driverTermsService) GetDriverTripTerms(ctx context.Context, tripID string) (DriverTerms, error) {
	found, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return DriverTerms{}, err
	}

	rate, err := s.terms.RateTerms(ctx, found.ID, found.RiderID)
	if err != nil {
		return DriverTerms{}, err
	}

	terms := DriverTerms{RateTerms: rate, ArrivalRadiusMeters: arrivalRadiusMeters}

	if found.ArrivedAt != nil {
		freeUntil := found.ArrivedAt.Add(time.Duration(rate.FreeWaitingMinutes) * time.Minute)
		noShowFrom := found.ArrivedAt.Add(time.Duration(rate.NoShowWaitMinutes) * time.Minute)
		terms.FreeWaitingUntil = &freeUntil
		terms.NoShowFrom = &noShowFrom
	}

	return terms, nil
}
