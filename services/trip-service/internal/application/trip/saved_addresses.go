package trip

import (
	"context"
	"fmt"
	"strings"
)

// SavedAddress is what a trip copies from one of the rider's saved addresses.
type SavedAddress struct {
	Coordinates  Coordinates
	Address      string
	Details      string
	Note         string
	PhotoMediaID string
}

// AddressBook reads the rider's saved addresses (rider-service). It returns
// ErrSavedAddressNotFound for an address that is not the rider's.
type AddressBook interface {
	SavedAddress(ctx context.Context, riderID, addressID string) (SavedAddress, error)
}

// WithSavedAddresses lets a trip be requested from or to one of the rider's
// saved addresses: its point and address are used, and for the pickup its
// details, note for the captain and photo are copied into the trip. It must
// wrap the base service directly, so every other decorator's RequestTrip
// reaches it.
func WithSavedAddresses(base Service, book AddressBook) Service {
	if base == nil {
		panic("trip service is required")
	}

	if book == nil {
		panic("address book is required")
	}

	return &savedAddressService{Service: base, book: book}
}

type savedAddressService struct {
	Service

	book AddressBook
}

func (s *savedAddressService) Unwrap() Service { return s.Service }

func (s *savedAddressService) RequestTrip(ctx context.Context, input RequestTripInput) (Trip, error) {
	riderID := strings.TrimSpace(input.RiderID)

	if id := strings.TrimSpace(input.PickupSavedAddressID); id != "" {
		saved, err := s.lookup(ctx, riderID, id)
		if err != nil {
			return Trip{}, err
		}

		input.PickupLat, input.PickupLng = saved.Coordinates.Latitude, saved.Coordinates.Longitude
		input.PickupAddress = saved.Address
		input.PickupDetails = saved.Details
		input.PickupNote = saved.Note
		input.PickupPhotoMediaID = saved.PhotoMediaID
		input.PickupSavedAddressID = ""
	} else {
		// Only a saved address can bring details, a note or a photo.
		input.PickupDetails, input.PickupNote, input.PickupPhotoMediaID = "", "", ""
	}

	if id := strings.TrimSpace(input.DropoffSavedAddressID); id != "" {
		saved, err := s.lookup(ctx, riderID, id)
		if err != nil {
			return Trip{}, err
		}

		input.DropoffLat, input.DropoffLng = saved.Coordinates.Latitude, saved.Coordinates.Longitude
		input.DropoffAddress = saved.Address
		input.DropoffSavedAddressID = ""
	}

	return s.Service.RequestTrip(ctx, input)
}

func (s *savedAddressService) lookup(ctx context.Context, riderID, addressID string) (SavedAddress, error) {
	if riderID == "" {
		return SavedAddress{}, ErrRiderIDRequired
	}

	if !looksLikeUUID(addressID) {
		return SavedAddress{}, ErrSavedAddressNotFound
	}

	saved, err := s.book.SavedAddress(ctx, riderID, addressID)
	if err != nil {
		return SavedAddress{}, fmt.Errorf("read saved address: %w", err)
	}

	return saved, nil
}
