package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// handlerOffersFake is a trip.Service that can also serve offers. The embedded
// Service is left nil: any other method would panic.
type handlerOffersFake struct {
	trip.Service

	offer    trip.Offer
	accepted trip.Trip
	err      error

	offered  []offerAsk
	pending  []string
	answered []offerAsk
}

type offerAsk struct {
	trip   string
	driver string
	ttl    time.Duration
	kind   string
}

func (f *handlerOffersFake) OfferTrip(_ context.Context, tripID string, driverID string, ttl time.Duration) (trip.Offer, error) {
	f.offered = append(f.offered, offerAsk{trip: tripID, driver: driverID, ttl: ttl})

	return f.offer, f.err
}

func (f *handlerOffersFake) GetPendingOffer(_ context.Context, driverID string) (trip.Offer, error) {
	f.pending = append(f.pending, driverID)

	return f.offer, f.err
}

func (f *handlerOffersFake) AcceptOffer(_ context.Context, tripID string, driverID string) (trip.Trip, error) {
	f.answered = append(f.answered, offerAsk{trip: tripID, driver: driverID, kind: "accept"})

	return f.accepted, f.err
}

func (f *handlerOffersFake) RejectOffer(_ context.Context, tripID string, driverID string) error {
	f.answered = append(f.answered, offerAsk{trip: tripID, driver: driverID, kind: "reject"})

	return f.err
}

var (
	offerAt     = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	offerExpiry = offerAt.Add(15 * time.Second)
)

func newOffersHandler(service trip.Service) *TripHandler {
	return newHistoryHandler(service)
}

func TestOfferTripPassesTheTTLAndReturnsTheTimes(t *testing.T) {
	fake := &handlerOffersFake{offer: trip.Offer{TripID: "trip-1", DriverID: "driver-a", OfferedAt: offerAt, ExpiresAt: offerExpiry}}

	response, err := newOffersHandler(fake).OfferTrip(context.Background(), &tripv1.OfferTripRequest{TripId: "trip-1", DriverId: "driver-a", TtlSeconds: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.GetTripId() != "trip-1" || response.GetDriverId() != "driver-a" || !response.GetExpiresAt().AsTime().Equal(offerExpiry) || !response.GetOfferedAt().AsTime().Equal(offerAt) {
		t.Errorf("wrong response: %+v", response)
	}

	if len(fake.offered) != 1 || fake.offered[0] != (offerAsk{trip: "trip-1", driver: "driver-a", ttl: 20 * time.Second}) {
		t.Errorf("unexpected question: %+v", fake.offered)
	}
}

func TestANegativeTTLIsInvalidAndNeverReachesTheService(t *testing.T) {
	fake := &handlerOffersFake{}

	_, err := newOffersHandler(fake).OfferTrip(context.Background(), &tripv1.OfferTripRequest{TripId: "trip-1", DriverId: "driver-a", TtlSeconds: -1})

	if status.Code(err) != codes.InvalidArgument || len(fake.offered) != 0 {
		t.Errorf("expected InvalidArgument without reaching the service: %v, %+v", status.Code(err), fake.offered)
	}
}

func TestGetPendingOfferShowsTheDriverWhatTheyNeedAndNoMore(t *testing.T) {
	fake := &handlerOffersFake{offer: trip.Offer{
		TripID: "trip-1", DriverID: "driver-a", OfferedAt: offerAt, ExpiresAt: offerExpiry,
		Trip: trip.Trip{
			ID: "trip-1", RiderID: "rider-secret", VehicleClass: "comfort", PaymentMethod: "wallet",
			Pickup: trip.Coordinates{Latitude: 36.19, Longitude: 44.01}, Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.02},
		},
	}}

	response, err := newOffersHandler(fake).GetPendingOffer(context.Background(), &tripv1.GetPendingOfferRequest{DriverId: "driver-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	offer := response.GetOffer()

	if offer.GetTripId() != "trip-1" || offer.GetVehicleClass() != "comfort" || offer.GetPaymentMethod() != "wallet" {
		t.Errorf("wrong offer: %+v", offer)
	}

	if offer.GetPickup().GetLatitude() != 36.19 || offer.GetDropoff().GetLongitude() != 44.02 {
		t.Errorf("wrong route: %+v %+v", offer.GetPickup(), offer.GetDropoff())
	}

	if !offer.GetExpiresAt().AsTime().Equal(offerExpiry) || !offer.GetOfferedAt().AsTime().Equal(offerAt) {
		t.Errorf("wrong times: %+v", offer)
	}

	if len(fake.pending) != 1 || fake.pending[0] != "driver-a" {
		t.Errorf("unexpected question: %v", fake.pending)
	}
}

func TestAcceptAndRejectPassTheAnswerThrough(t *testing.T) {
	fake := &handlerOffersFake{accepted: trip.Trip{ID: "trip-1", DriverID: "driver-a", Status: trip.StatusAccepted}}
	handler := newOffersHandler(fake)

	accepted, err := handler.AcceptOffer(context.Background(), &tripv1.AcceptOfferRequest{TripId: "trip-1", DriverId: "driver-a"})
	if err != nil || accepted.GetTrip().GetId() != "trip-1" || accepted.GetTrip().GetDriverId() != "driver-a" {
		t.Errorf("AcceptOffer: %+v, %v", accepted, err)
	}

	if _, err := handler.RejectOffer(context.Background(), &tripv1.RejectOfferRequest{TripId: "trip-1", DriverId: "driver-a"}); err != nil {
		t.Errorf("RejectOffer: %v", err)
	}

	want := []offerAsk{{trip: "trip-1", driver: "driver-a", kind: "accept"}, {trip: "trip-1", driver: "driver-a", kind: "reject"}}
	if len(fake.answered) != 2 || fake.answered[0] != want[0] || fake.answered[1] != want[1] {
		t.Errorf("unexpected questions: %+v", fake.answered)
	}
}

func TestEachRefusalHasItsOwnCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"no trip id", trip.ErrTripIDRequired, codes.InvalidArgument},
		{"no driver id", trip.ErrDriverIDRequired, codes.InvalidArgument},
		{"no offer", trip.ErrOfferNotFound, codes.NotFound},
		{"an expired offer", trip.ErrOfferExpired, codes.FailedPrecondition},
		{"another driver is being offered the trip", trip.ErrOfferInProgress, codes.Aborted},
		{"already offered to this driver", trip.ErrAlreadyOffered, codes.AlreadyExists},
		{"the driver has a pending offer", trip.ErrDriverHasPendingOffer, codes.FailedPrecondition},
		{"the trip is not waiting for a driver", trip.ErrTripNotOfferable, codes.FailedPrecondition},
		{"the driver is on a trip", trip.ErrDriverHasActiveTrip, codes.FailedPrecondition},
		{"the trip was cancelled meanwhile", trip.ErrInvalidTransition, codes.FailedPrecondition},
		{"the trip does not exist", trip.ErrTripNotFound, codes.NotFound},
		{"a wrapped refusal", errors.Join(errors.New("context"), trip.ErrOfferInProgress), codes.Aborted},
		{"anything else", errors.New("database down"), codes.Internal},
	}

	for _, tc := range cases {
		handler := newOffersHandler(&handlerOffersFake{err: tc.err})
		ctx := context.Background()

		_, offerErr := handler.OfferTrip(ctx, &tripv1.OfferTripRequest{TripId: "t", DriverId: "d"})
		_, getErr := handler.GetPendingOffer(ctx, &tripv1.GetPendingOfferRequest{DriverId: "d"})
		_, acceptErr := handler.AcceptOffer(ctx, &tripv1.AcceptOfferRequest{TripId: "t", DriverId: "d"})
		_, rejectErr := handler.RejectOffer(ctx, &tripv1.RejectOfferRequest{TripId: "t", DriverId: "d"})

		for rpc, err := range map[string]error{"OfferTrip": offerErr, "GetPendingOffer": getErr, "AcceptOffer": acceptErr, "RejectOffer": rejectErr} {
			if got := status.Code(err); got != tc.want {
				t.Errorf("%s / %s: expected %v, got %v", tc.name, rpc, tc.want, got)
			}
		}
	}
}

func TestOffersWithoutThemConfiguredAreUnimplemented(t *testing.T) {
	handler := newOffersHandler(struct{ trip.Service }{})
	ctx := context.Background()

	_, offerErr := handler.OfferTrip(ctx, &tripv1.OfferTripRequest{TripId: "t", DriverId: "d"})
	_, getErr := handler.GetPendingOffer(ctx, &tripv1.GetPendingOfferRequest{DriverId: "d"})
	_, acceptErr := handler.AcceptOffer(ctx, &tripv1.AcceptOfferRequest{TripId: "t", DriverId: "d"})
	_, rejectErr := handler.RejectOffer(ctx, &tripv1.RejectOfferRequest{TripId: "t", DriverId: "d"})

	for rpc, err := range map[string]error{"OfferTrip": offerErr, "GetPendingOffer": getErr, "AcceptOffer": acceptErr, "RejectOffer": rejectErr} {
		if status.Code(err) != codes.Unimplemented {
			t.Errorf("%s: expected Unimplemented, got %v", rpc, status.Code(err))
		}
	}
}

func TestOffersAreFoundBehindOtherDecorators(t *testing.T) {
	stacked := &wrappedHistoryLayer{Service: &handlerOffersFake{offer: trip.Offer{TripID: "trip-1"}}}

	response, err := newOffersHandler(stacked).GetPendingOffer(context.Background(), &tripv1.GetPendingOfferRequest{DriverId: "driver-a"})
	if err != nil || response.GetOffer().GetTripId() != "trip-1" {
		t.Errorf("offers behind another decorator must still be served: %+v, %v", response, err)
	}
}

func TestEveryOfferRPCRequiresARequest(t *testing.T) {
	handler := newOffersHandler(&handlerOffersFake{})
	ctx := context.Background()

	_, offerErr := handler.OfferTrip(ctx, nil)
	_, getErr := handler.GetPendingOffer(ctx, nil)
	_, acceptErr := handler.AcceptOffer(ctx, nil)
	_, rejectErr := handler.RejectOffer(ctx, nil)

	for rpc, err := range map[string]error{"OfferTrip": offerErr, "GetPendingOffer": getErr, "AcceptOffer": acceptErr, "RejectOffer": rejectErr} {
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", rpc, status.Code(err))
		}
	}
}
