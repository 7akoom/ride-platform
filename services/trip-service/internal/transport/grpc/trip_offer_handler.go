package grpc

import (
	"context"
	"errors"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// tripOffers is what trip.WithTripOffers adds to a trip.Service. The handler asks
// for it as an optional capability (trip.As), so the Service interface and every
// fake of it stay as they are.
type tripOffers interface {
	OfferTrip(ctx context.Context, tripID string, driverID string, ttl time.Duration) (trip.Offer, error)
	GetPendingOffer(ctx context.Context, driverID string) (trip.Offer, error)
	AcceptOffer(ctx context.Context, tripID string, driverID string) (trip.Trip, error)
	RejectOffer(ctx context.Context, tripID string, driverID string) error
}

func (h *TripHandler) offers() (tripOffers, error) {
	offers, ok := trip.As[tripOffers](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip offers are not configured")
	}

	return offers, nil
}

// OfferTrip puts a trip to one driver for a short time. Internal: only dispatch
// calls it, and the interceptor refuses everyone else.
func (h *TripHandler) OfferTrip(
	ctx context.Context,
	request *tripv1.OfferTripRequest,
) (*tripv1.OfferTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if request.GetTtlSeconds() < 0 {
		return nil, status.Error(codes.InvalidArgument, "ttl_seconds must not be negative")
	}

	offers, err := h.offers()
	if err != nil {
		return nil, err
	}

	offer, err := offers.OfferTrip(ctx, request.GetTripId(), request.GetDriverId(), time.Duration(request.GetTtlSeconds())*time.Second)
	if err != nil {
		return nil, h.mapOfferError(err)
	}

	return &tripv1.OfferTripResponse{
		TripId:    offer.TripID,
		DriverId:  offer.DriverID,
		OfferedAt: timestamppb.New(offer.OfferedAt),
		ExpiresAt: timestamppb.New(offer.ExpiresAt),
	}, nil
}

// GetPendingOffer is the driver app's poll target: the trip it is being offered,
// with what it needs to decide, or NOT_FOUND. The interceptor has already checked
// that the driver named is the caller's own profile.
func (h *TripHandler) GetPendingOffer(
	ctx context.Context,
	request *tripv1.GetPendingOfferRequest,
) (*tripv1.GetPendingOfferResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	offers, err := h.offers()
	if err != nil {
		return nil, err
	}

	offer, err := offers.GetPendingOffer(ctx, request.GetDriverId())
	if err != nil {
		return nil, h.mapOfferError(err)
	}

	return &tripv1.GetPendingOfferResponse{Offer: toProtoOffer(offer)}, nil
}

// AcceptOffer makes the caller the driver of the trip. The interceptor has already
// checked that the driver named is the caller's own profile.
func (h *TripHandler) AcceptOffer(
	ctx context.Context,
	request *tripv1.AcceptOfferRequest,
) (*tripv1.AcceptOfferResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	offers, err := h.offers()
	if err != nil {
		return nil, err
	}

	accepted, err := offers.AcceptOffer(ctx, request.GetTripId(), request.GetDriverId())
	if err != nil {
		return nil, h.mapOfferError(err)
	}

	return &tripv1.AcceptOfferResponse{Trip: toProtoTrip(accepted)}, nil
}

// RejectOffer declines the offer; the trip goes on to the next driver.
func (h *TripHandler) RejectOffer(
	ctx context.Context,
	request *tripv1.RejectOfferRequest,
) (*tripv1.RejectOfferResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	offers, err := h.offers()
	if err != nil {
		return nil, err
	}

	if err := offers.RejectOffer(ctx, request.GetTripId(), request.GetDriverId()); err != nil {
		return nil, h.mapOfferError(err)
	}

	return &tripv1.RejectOfferResponse{}, nil
}

// mapOfferError gives each refusal its own code, so dispatch can tell "skip this
// driver" (ALREADY_EXISTS, FAILED_PRECONDITION) from "wait, another driver is
// being offered this trip" (ABORTED).
func (h *TripHandler) mapOfferError(err error) error {
	switch {
	case errors.Is(err, trip.ErrTripIDRequired), errors.Is(err, trip.ErrDriverIDRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, trip.ErrOfferNotFound):
		return status.Error(codes.NotFound, "no pending offer")

	case errors.Is(err, trip.ErrOfferExpired):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, trip.ErrOfferInProgress):
		return status.Error(codes.Aborted, err.Error())

	case errors.Is(err, trip.ErrAlreadyOffered):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, trip.ErrDriverHasPendingOffer), errors.Is(err, trip.ErrTripNotOfferable):
		return status.Error(codes.FailedPrecondition, err.Error())

	default:
		return h.mapTripError(err)
	}
}

// toProtoOffer shows the driver what they need to decide, and no more: not who the
// rider is.
func toProtoOffer(offer trip.Offer) *tripv1.TripOffer {
	return &tripv1.TripOffer{
		TripId:        offer.TripID,
		Pickup:        &tripv1.Coordinates{Latitude: offer.Trip.Pickup.Latitude, Longitude: offer.Trip.Pickup.Longitude},
		Dropoff:       &tripv1.Coordinates{Latitude: offer.Trip.Dropoff.Latitude, Longitude: offer.Trip.Dropoff.Longitude},
		VehicleClass:  offer.Trip.VehicleClass,
		PaymentMethod: offer.Trip.PaymentMethod,
		OfferedAt:     timestamppb.New(offer.OfferedAt),
		ExpiresAt:     timestamppb.New(offer.ExpiresAt),
		// Where, not what the rider told the captain: the note and the photo
		// come with the trip once the driver has accepted it.
		PickupAddress:  offer.Trip.PickupAddress,
		DropoffAddress: offer.Trip.DropoffAddress,
		// What the trip pays, when it was quoted.
		QuotedFare:   offer.Trip.QuotedFare,
		CurrencyCode: offer.Trip.CurrencyCode,
		// Where it stops on the way: the driver should know before accepting.
		Stops: toProtoStops(offer.Trip.Stops),
	}
}
