package grpc

import (
	"context"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// handlerChainedOffersFake can also offer a trip after the driver's current one.
type handlerChainedOffersFake struct {
	handlerOffersFake

	chains []trip.OfferChain
}

func (f *handlerChainedOffersFake) OfferTripWithChain(_ context.Context, tripID string, driverID string, ttl time.Duration, chain trip.OfferChain) (trip.Offer, error) {
	f.offered = append(f.offered, offerAsk{trip: tripID, driver: driverID, ttl: ttl})
	f.chains = append(f.chains, chain)

	return f.offer, f.err
}

func TestOfferTripPassesTheTripBeforeAndTheRoadTime(t *testing.T) {
	fake := &handlerChainedOffersFake{handlerOffersFake: handlerOffersFake{offer: trip.Offer{TripID: "trip-2", OfferedAt: offerAt, ExpiresAt: offerExpiry}}}

	_, err := newOffersHandler(fake).OfferTrip(context.Background(), &tripv1.OfferTripRequest{
		TripId: "trip-2", DriverId: "driver-a", AfterTripId: "trip-1", PickupEtaSeconds: 480,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(fake.chains) != 1 || fake.chains[0].AfterTripID != "trip-1" || fake.chains[0].PickupETA != 8*time.Minute {
		t.Fatalf("chain %+v", fake.chains)
	}
}

func TestOfferTripRefusesANegativeRoadTime(t *testing.T) {
	fake := &handlerChainedOffersFake{}

	_, err := newOffersHandler(fake).OfferTrip(context.Background(), &tripv1.OfferTripRequest{TripId: "trip-2", DriverId: "driver-a", PickupEtaSeconds: -1})
	if status.Code(err) != codes.InvalidArgument || len(fake.offered) != 0 {
		t.Fatalf("got %v, %d calls", err, len(fake.offered))
	}
}

func TestOfferTripAfterATripNeedsChainedOffers(t *testing.T) {
	fake := &handlerOffersFake{}

	_, err := newOffersHandler(fake).OfferTrip(context.Background(), &tripv1.OfferTripRequest{TripId: "trip-2", DriverId: "driver-a", AfterTripId: "trip-1"})
	if status.Code(err) != codes.FailedPrecondition || len(fake.offered) != 0 {
		t.Fatalf("got %v, %d calls", err, len(fake.offered))
	}
}

func TestTheDriverSeesAnOfferIsAfterTheirTrip(t *testing.T) {
	offer := toProtoOffer(trip.Offer{TripID: "trip-2", AfterTripID: "trip-1", PickupETA: 8 * time.Minute, OfferedAt: offerAt, ExpiresAt: offerExpiry})

	if !offer.GetAfterCurrentTrip() || offer.GetPickupEtaSeconds() != 480 {
		t.Fatalf("offer %+v", offer)
	}
}

func TestAWaitingTripSaysSo(t *testing.T) {
	expected := offerAt.Add(8 * time.Minute)
	shown := toProtoTrip(trip.Trip{ID: "trip-2", AfterTripID: "trip-1", ExpectedPickupAt: &expected})

	if !shown.GetWaitingForPreviousTrip() || shown.GetHeadingToPickupAt() != nil || !shown.GetExpectedPickupAt().AsTime().Equal(expected) {
		t.Fatalf("trip %+v", shown)
	}
}

// handlerNextTripFake also knows the driver's next trip.
type handlerNextTripFake struct {
	handlerHistoryFake

	next     trip.Trip
	nextErr  error
	askedFor []string
}

func (f *handlerNextTripFake) NextTrip(_ context.Context, driverID string) (trip.Trip, error) {
	f.askedFor = append(f.askedFor, driverID)

	return f.next, f.nextErr
}

func TestADriverSeesTheirNextTripBesideTheCurrentOne(t *testing.T) {
	fake := &handlerNextTripFake{
		handlerHistoryFake: handlerHistoryFake{active: trip.Trip{ID: "trip-1"}},
		next:               trip.Trip{ID: "trip-2", AfterTripID: "trip-1"},
	}

	response, err := newHistoryHandler(fake).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{DriverId: "driver-a"})
	if err != nil {
		t.Fatal(err)
	}

	if response.GetNextTrip().GetId() != "trip-2" || !response.GetNextTrip().GetWaitingForPreviousTrip() {
		t.Fatalf("next trip %+v", response.GetNextTrip())
	}
}

func TestARiderIsNeverToldOfANextTrip(t *testing.T) {
	fake := &handlerNextTripFake{handlerHistoryFake: handlerHistoryFake{active: trip.Trip{ID: "trip-2"}}, next: trip.Trip{ID: "trip-3"}}

	response, err := newHistoryHandler(fake).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{RiderId: "rider-a"})
	if err != nil {
		t.Fatal(err)
	}

	if response.GetNextTrip() != nil || len(fake.askedFor) != 0 {
		t.Fatalf("next trip %+v, asked %v", response.GetNextTrip(), fake.askedFor)
	}
}

func TestADriverWithoutANextTripGetsNone(t *testing.T) {
	fake := &handlerNextTripFake{handlerHistoryFake: handlerHistoryFake{active: trip.Trip{ID: "trip-1"}}, nextErr: trip.ErrTripNotFound}

	response, err := newHistoryHandler(fake).GetActiveTrip(context.Background(), &tripv1.GetActiveTripRequest{DriverId: "driver-a"})
	if err != nil || response.GetNextTrip() != nil {
		t.Fatalf("got %+v, %v", response.GetNextTrip(), err)
	}
}
