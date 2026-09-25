package schedule

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	maxKeyLength     = 120
	maxAddressLength = 300
	listLimit        = 50
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IDGenerator makes a booking's id (the trip it becomes has the same one).
type IDGenerator interface {
	NewID() string
}

// Service books, lists and cancels trips booked ahead.
type Service struct {
	store  Store
	zones  ZoneLocator
	book   trip.AddressBook
	ids    IDGenerator
	limits Limits
	now    func() time.Time
}

func NewService(store Store, zones ZoneLocator, book trip.AddressBook, ids IDGenerator, limits Limits) *Service {
	switch {
	case store == nil:
		panic("schedule store is required")
	case zones == nil:
		panic("zone locator is required")
	case book == nil:
		panic("address book is required")
	case ids == nil:
		panic("id generator is required")
	case limits.MinAhead <= 0 || limits.MaxAhead <= limits.MinAhead || limits.MaxUpcoming < 1 ||
		limits.DispatchLead <= 0 || limits.DispatchLead >= limits.MinAhead || limits.Grace <= 0:
		panic("schedule limits are not consistent")
	}

	return &Service{store: store, zones: zones, book: book, ids: ids, limits: limits, now: func() time.Time { return time.Now().UTC() }}
}

// BookInput is a rider booking a trip ahead.
type BookInput struct {
	RiderID               string
	PickupLat             float64
	PickupLng             float64
	DropoffLat            float64
	DropoffLng            float64
	PickupAddress         string
	DropoffAddress        string
	PickupSavedAddressID  string
	DropoffSavedAddressID string
	VehicleClass          string
	PaymentMethod         string
	PassengerName         string
	PassengerPhone        string
	ScheduledAt           time.Time
	IdempotencyKey        string
}

// Book checks and stores a booking. A retry with the same key returns the
// booking made the first time.
func (s *Service) Book(ctx context.Context, input BookInput) (Ride, error) {
	rider := strings.TrimSpace(input.RiderID)
	key := strings.TrimSpace(input.IdempotencyKey)
	now := s.now()

	switch {
	case rider == "":
		return Ride{}, ErrRiderRequired
	case key == "" || len(key) > maxKeyLength:
		return Ride{}, ErrIdempotencyKey
	case input.ScheduledAt.Before(now.Add(s.limits.MinAhead)):
		return Ride{}, ErrTooSoon
	case input.ScheduledAt.After(now.Add(s.limits.MaxAhead)):
		return Ride{}, ErrTooFar
	}

	if done, found, err := s.store.FindByKey(ctx, rider, key); err != nil {
		return Ride{}, fmt.Errorf("look up the idempotency key: %w", err)
	} else if found {
		return s.replay(done, input)
	}

	vehicleClass, err := trip.NormalizeVehicleClass(input.VehicleClass)
	if err != nil {
		return Ride{}, err
	}

	paymentMethod, err := trip.NormalizePaymentMethod(input.PaymentMethod)
	if err != nil {
		return Ride{}, err
	}

	passengerName, passengerPhone, err := trip.NormalizePassenger(input.PassengerName, input.PassengerPhone)
	if err != nil {
		return Ride{}, err
	}

	ride := Ride{
		ID:             s.ids.NewID(),
		RiderID:        rider,
		IdempotencyKey: key,
		Status:         Scheduled,
		ScheduledAt:    input.ScheduledAt.UTC(),
		PickupAddress:  strings.TrimSpace(input.PickupAddress),
		DropoffAddress: strings.TrimSpace(input.DropoffAddress),
		VehicleClass:   vehicleClass,
		PaymentMethod:  paymentMethod,
		PassengerName:  passengerName,
		PassengerPhone: passengerPhone,
		NextAttemptAt:  input.ScheduledAt.UTC().Add(-s.limits.DispatchLead),
	}

	pickupLat, pickupLng := input.PickupLat, input.PickupLng
	dropoffLat, dropoffLng := input.DropoffLat, input.DropoffLng

	// A saved address is read now for its point and address (kept in case
	// it is gone by the time), and again when the trip is dispatched.
	if id := strings.TrimSpace(input.PickupSavedAddressID); id != "" {
		saved, err := s.savedAddress(ctx, rider, id)
		if err != nil {
			return Ride{}, err
		}

		pickupLat, pickupLng, ride.PickupAddress, ride.PickupSavedAddressID = saved.Coordinates.Latitude, saved.Coordinates.Longitude, saved.Address, id
	}

	if id := strings.TrimSpace(input.DropoffSavedAddressID); id != "" {
		saved, err := s.savedAddress(ctx, rider, id)
		if err != nil {
			return Ride{}, err
		}

		dropoffLat, dropoffLng, ride.DropoffAddress, ride.DropoffSavedAddressID = saved.Coordinates.Latitude, saved.Coordinates.Longitude, saved.Address, id
	}

	if utf8.RuneCountInString(ride.PickupAddress) > maxAddressLength || utf8.RuneCountInString(ride.DropoffAddress) > maxAddressLength {
		return Ride{}, trip.ErrAddressTooLong
	}

	if ride.Pickup, err = trip.NewCoordinates(pickupLat, pickupLng); err != nil {
		return Ride{}, err
	}

	if ride.Dropoff, err = trip.NewCoordinates(dropoffLat, dropoffLng); err != nil {
		return Ride{}, err
	}

	served, timeZone, err := s.zones.Locate(ctx, ride.Pickup.Latitude, ride.Pickup.Longitude)
	if err != nil {
		return Ride{}, fmt.Errorf("check pickup service zone: %w", err)
	}

	if !served {
		return Ride{}, trip.ErrPickupOutsideServiceZone
	}

	ride.TimeZone = timeZone
	if ride.TimeZone == "" {
		ride.TimeZone = "UTC"
	}

	created, existed, err := s.store.Create(ctx, ride, s.limits.MaxUpcoming)
	if err != nil {
		return Ride{}, err
	}

	if existed {
		return s.replay(created, input)
	}

	return created, nil
}

func (s *Service) savedAddress(ctx context.Context, riderID, id string) (trip.SavedAddress, error) {
	if !uuidPattern.MatchString(id) {
		return trip.SavedAddress{}, trip.ErrSavedAddressNotFound
	}

	saved, err := s.book.SavedAddress(ctx, riderID, id)
	if err != nil {
		return trip.SavedAddress{}, fmt.Errorf("read saved address: %w", err)
	}

	return saved, nil
}

// replay is the booking already made with the key: the same one again, or
// ErrKeyReused for another.
func (s *Service) replay(done Ride, input BookInput) (Ride, error) {
	if !done.ScheduledAt.Equal(input.ScheduledAt.UTC()) {
		return Ride{}, ErrKeyReused
	}

	if input.PickupSavedAddressID == "" && (math.Abs(done.Pickup.Latitude-input.PickupLat) > 1e-9 || math.Abs(done.Pickup.Longitude-input.PickupLng) > 1e-9) {
		return Ride{}, ErrKeyReused
	}

	return done, nil
}

func (s *Service) List(ctx context.Context, riderID string, includePast bool) ([]Ride, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return nil, ErrRiderRequired
	}

	return s.store.ListForRider(ctx, riderID, includePast, listLimit)
}

func (s *Service) Cancel(ctx context.Context, id, riderID string) (Ride, error) {
	id, riderID = strings.TrimSpace(id), strings.TrimSpace(riderID)

	switch {
	case riderID == "":
		return Ride{}, ErrRiderRequired
	case id == "":
		return Ride{}, ErrNotFound
	}

	return s.store.Cancel(ctx, id, riderID, s.now())
}

// failureReason is what the rider reads about a booking that could not be
// dispatched: the trip service's own reasons as they are, anything else
// (a service down) plainly.
func failureReason(err error) string {
	for _, known := range []error{
		trip.ErrRiderHasActiveTrip,
		trip.ErrRiderOwesFees,
		trip.ErrPickupOutsideServiceZone,
		trip.ErrSavedAddressNotFound,
		trip.ErrInvalidPassenger,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}

	return "the trip could not be requested"
}
