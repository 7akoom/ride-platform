package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

func (s *service) RequestTrip(
	ctx context.Context,
	input RequestTripInput,
) (Trip, error) {
	riderID := strings.TrimSpace(input.RiderID)
	if riderID == "" {
		return Trip{}, ErrRiderIDRequired
	}

	vehicleClass, err := NormalizeVehicleClass(input.VehicleClass)
	if err != nil {
		return Trip{}, err
	}

	paymentMethod, err := NormalizePaymentMethod(input.PaymentMethod)
	if err != nil {
		return Trip{}, err
	}

	if input.PickupSavedAddressID != "" || input.DropoffSavedAddressID != "" {
		return Trip{}, ErrSavedAddressesUnavailable
	}

	quoteID := strings.TrimSpace(input.QuoteID)
	if quoteID != "" {
		if s.quotes == nil {
			return Trip{}, ErrQuotesUnavailable
		}

		if !looksLikeUUID(quoteID) {
			return Trip{}, ErrQuoteNotFound
		}
	}

	texts, err := tripTexts(input)
	if err != nil {
		return Trip{}, err
	}

	pickup, err := NewCoordinates(input.PickupLat, input.PickupLng)
	if err != nil {
		return Trip{}, err
	}

	dropoff, err := NewCoordinates(input.DropoffLat, input.DropoffLng)
	if err != nil {
		return Trip{}, err
	}

	served, err := s.zoneChecker.CheckServiceZone(ctx, pickup.Latitude, pickup.Longitude)
	if err != nil {
		return Trip{}, fmt.Errorf("check pickup service zone: %w", err)
	}

	if !served {
		return Trip{}, ErrPickupOutsideServiceZone
	}

	_, err = s.repository.FindActiveByRiderID(ctx, riderID)
	switch {
	case err == nil:
		return Trip{}, ErrRiderHasActiveTrip
	case errors.Is(err, ErrTripNotFound):
		// Expected path: rider has no active trip right now.
	default:
		return Trip{}, fmt.Errorf("check rider's active trip: %w", err)
	}

	create := CreateInput{
		ID:            s.idGenerator.NewID(),
		RiderID:       riderID,
		Pickup:        pickup,
		Dropoff:       dropoff,
		VehicleClass:  vehicleClass,
		PaymentMethod: paymentMethod,

		PickupAddress:      texts.PickupAddress,
		DropoffAddress:     texts.DropoffAddress,
		PickupDetails:      texts.PickupDetails,
		PickupNote:         texts.PickupNote,
		PickupPhotoMediaID: texts.PickupPhotoMediaID,
	}

	if quoteID != "" {
		quote, err := s.quotes.Claim(ctx, quoteID, riderID, create.ID)
		if err != nil {
			return Trip{}, fmt.Errorf("claim quote: %w", err)
		}

		if !matchesQuote(quote, pickup, dropoff, input.VehicleClass) {
			s.releaseQuote(ctx, quoteID, create.ID)

			return Trip{}, ErrQuoteMismatch
		}

		create.VehicleClass = quote.VehicleClass
		create.QuoteID = quote.ID
		create.QuotedFare = quote.Total
		create.CurrencyCode = quote.CurrencyCode
	}

	created, err := s.repository.Create(ctx, create)
	if err != nil {
		if quoteID != "" {
			s.releaseQuote(ctx, quoteID, create.ID)
		}

		return Trip{}, fmt.Errorf("create trip: %w", err)
	}

	return created, nil
}

// releaseQuote frees a quote whose trip will not be created, so the rider
// can still use it. Best effort: if it fails, the rider asks for a new quote.
func (s *service) releaseQuote(ctx context.Context, quoteID, tripID string) {
	_ = s.quotes.Release(context.WithoutCancel(ctx), quoteID, tripID)
}

// Longest texts a trip keeps, the same as a saved address allows.
const (
	maxAddressLength = 300
	maxDetailsLength = 200
	maxNoteLength    = 300
)

// tripTexts trims the addresses and the pickup details and note, and refuses
// ones too long to keep.
func tripTexts(input RequestTripInput) (CreateInput, error) {
	out := CreateInput{
		PickupAddress:      strings.TrimSpace(input.PickupAddress),
		DropoffAddress:     strings.TrimSpace(input.DropoffAddress),
		PickupDetails:      strings.TrimSpace(input.PickupDetails),
		PickupNote:         strings.TrimSpace(input.PickupNote),
		PickupPhotoMediaID: strings.TrimSpace(input.PickupPhotoMediaID),
	}

	switch {
	case utf8.RuneCountInString(out.PickupAddress) > maxAddressLength,
		utf8.RuneCountInString(out.DropoffAddress) > maxAddressLength,
		utf8.RuneCountInString(out.PickupDetails) > maxDetailsLength,
		utf8.RuneCountInString(out.PickupNote) > maxNoteLength:
		return CreateInput{}, ErrAddressTooLong
	case out.PickupPhotoMediaID != "" && !looksLikeUUID(out.PickupPhotoMediaID):
		return CreateInput{}, ErrSavedAddressNotFound
	}

	return out, nil
}
