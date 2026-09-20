package trip

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// DefaultHistoryPageSize is used when a list request does not say.
	DefaultHistoryPageSize = 20

	// MaxHistoryPageSize is the most trips one page can hold; a larger request is
	// served with this many rather than refused.
	MaxHistoryPageSize = 50

	pageTokenPrefix = "t1:"
)

var (
	// ErrInvalidHistoryQuery means the request did not name exactly one of a rider
	// and a driver. Who may ask about whom is decided before this is reached.
	ErrInvalidHistoryQuery = errors.New("exactly one of rider_id and driver_id is required")

	// ErrInvalidPageSize means a negative page size.
	ErrInvalidPageSize = errors.New("page_size must not be negative")

	// ErrInvalidPageToken means the page token is not one a previous page returned.
	ErrInvalidPageToken = errors.New("page_token is not valid")
)

// HistoryStore is the outbound port behind a user's active trip and trip list.
// ListBy* return the owner's trips newest first, starting after the trip with the
// given id (or from the newest when it is empty), at most limit of them. A trip id
// that is not the owner's is not an error: it simply yields no trips.
type HistoryStore interface {
	FindActiveByRiderID(ctx context.Context, riderID string) (Trip, error)
	FindActiveByDriverID(ctx context.Context, driverID string) (Trip, error)
	ListByRiderID(ctx context.Context, riderID string, afterTripID string, limit int) ([]Trip, error)
	ListByDriverID(ctx context.Context, driverID string, afterTripID string, limit int) ([]Trip, error)
}

// HistoryQuery asks for one page of a rider's or a driver's trips.
type HistoryQuery struct {
	RiderID   string
	DriverID  string
	PageSize  int
	PageToken string
}

// HistoryPage is one page of trips, newest first. NextPageToken is empty on the
// last page.
type HistoryPage struct {
	Trips         []Trip
	NextPageToken string
}

// WithTripHistory decorates a Service with GetActiveTrip and ListTrips, which is
// how an app finds the trip it should be showing when it is opened again, and how
// it lists past trips. Every other method is the base service's. The transport
// layer reaches these through trip.As, so the Service interface (and every fake
// of it) stays unchanged.
func WithTripHistory(base Service, store HistoryStore) Service {
	if base == nil {
		panic("trip service is required")
	}

	if store == nil {
		panic("history store is required")
	}

	return &historyService{Service: base, store: store}
}

type historyService struct {
	Service

	store HistoryStore
}

// Unwrap returns the Service the history decorator wraps.
func (s *historyService) Unwrap() Service { return s.Service }

// GetActiveTrip returns the rider's or the driver's requested, accepted or
// in-progress trip, or ErrTripNotFound when there is none.
func (s *historyService) GetActiveTrip(ctx context.Context, riderID string, driverID string) (Trip, error) {
	switch {
	case riderID != "" && driverID == "":
		return s.store.FindActiveByRiderID(ctx, riderID)

	case driverID != "" && riderID == "":
		return s.store.FindActiveByDriverID(ctx, driverID)

	default:
		return Trip{}, ErrInvalidHistoryQuery
	}
}

// ListTrips returns one page of the rider's or the driver's trips, newest first.
func (s *historyService) ListTrips(ctx context.Context, query HistoryQuery) (HistoryPage, error) {
	if (query.RiderID == "") == (query.DriverID == "") {
		return HistoryPage{}, ErrInvalidHistoryQuery
	}

	size, err := historyPageSize(query.PageSize)
	if err != nil {
		return HistoryPage{}, err
	}

	after := ""
	if query.PageToken != "" {
		if after, err = decodePageToken(query.PageToken); err != nil {
			return HistoryPage{}, err
		}
	}

	// One more than a page, to know whether there is a next one.
	var trips []Trip

	if query.RiderID != "" {
		trips, err = s.store.ListByRiderID(ctx, query.RiderID, after, size+1)
	} else {
		trips, err = s.store.ListByDriverID(ctx, query.DriverID, after, size+1)
	}

	if err != nil {
		return HistoryPage{}, fmt.Errorf("list trips: %w", err)
	}

	page := HistoryPage{Trips: trips}

	if len(trips) > size {
		page.Trips = trips[:size]
		page.NextPageToken = encodePageToken(trips[size-1].ID)
	}

	return page, nil
}

func historyPageSize(requested int) (int, error) {
	switch {
	case requested < 0:
		return 0, ErrInvalidPageSize
	case requested == 0:
		return DefaultHistoryPageSize, nil
	case requested > MaxHistoryPageSize:
		return MaxHistoryPageSize, nil
	default:
		return requested, nil
	}
}

// A page token is the id of the last trip of the previous page, wrapped so that
// callers treat it as opaque. The next page starts after that trip.
func encodePageToken(lastTripID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(pageTokenPrefix + lastTripID))
}

func decodePageToken(token string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrInvalidPageToken
	}

	id, ok := strings.CutPrefix(string(raw), pageTokenPrefix)
	if !ok || !looksLikeUUID(id) {
		return "", ErrInvalidPageToken
	}

	return id, nil
}

// looksLikeUUID checks the canonical 8-4-4-4-12 form, so a forged token can never
// put anything but a well-formed id into a query.
func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isDigit := r >= '0' && r <= '9'
			isHex := (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')

			if !isDigit && !isHex {
				return false
			}
		}
	}

	return true
}
