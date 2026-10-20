package trip

import (
	"context"
	"errors"
	"testing"
	"time"
)

type chainedFakeStore struct {
	offersFakeStore

	chains []OfferChain
}

func (s *chainedFakeStore) CreateChainedOffer(_ context.Context, tripID, driverID string, ttl time.Duration, chain OfferChain) (Offer, error) {
	s.created = append(s.created, offerCall{tripID, driverID, ttl})
	s.chains = append(s.chains, chain)

	return s.offer, s.err
}

type chainedOffers interface {
	OfferTripWithChain(ctx context.Context, tripID string, driverID string, ttl time.Duration, chain OfferChain) (Offer, error)
}

const (
	currentTripID = "8f3c2b1a-1111-4c2d-9e8f-0a1b2c3d4e5f"
	newTripID     = "1a2b3c4d-2222-4c2d-9e8f-0a1b2c3d4e5f"
)

func TestAnOfferAfterATripReachesTheStoreAsAsked(t *testing.T) {
	store := &chainedFakeStore{}

	offers, ok := As[chainedOffers](WithTripOffers(&offersFakeBase{}, store))
	if !ok {
		t.Fatal("offers can be chained")
	}

	if _, err := offers.OfferTripWithChain(context.Background(), " "+newTripID+" ", "driver-1", 0, OfferChain{AfterTripID: " " + currentTripID, PickupETA: 9 * time.Minute}); err != nil {
		t.Fatal(err)
	}

	if len(store.chains) != 1 || store.chains[0].AfterTripID != currentTripID || store.chains[0].PickupETA != 9*time.Minute {
		t.Fatalf("chain %+v", store.chains)
	}

	if store.created[0].trip != newTripID || store.created[0].ttl != DefaultOfferTTL {
		t.Fatalf("call %+v", store.created[0])
	}
}

func TestAnOfferAfterATripIsRefusedWhenItCannotBeOne(t *testing.T) {
	cases := map[string]OfferChain{
		"after itself":  {AfterTripID: newTripID},
		"not a trip id": {AfterTripID: "x'; DROP TABLE trips"},
	}

	for name, chain := range cases {
		store := &chainedFakeStore{}
		offers, _ := As[chainedOffers](WithTripOffers(&offersFakeBase{}, store))

		if _, err := offers.OfferTripWithChain(context.Background(), newTripID, "driver-1", 0, chain); !errors.Is(err, ErrNotOfferableAfterTrip) {
			t.Errorf("%s: %v", name, err)
		}

		if len(store.created) != 0 {
			t.Errorf("%s: reached the store", name)
		}
	}
}

func TestAStoreThatCannotChainStillMakesOrdinaryOffers(t *testing.T) {
	store := &offersFakeStore{}
	offers, _ := As[chainedOffers](WithTripOffers(&offersFakeBase{}, store))

	if _, err := offers.OfferTripWithChain(context.Background(), newTripID, "driver-1", 0, OfferChain{PickupETA: time.Minute}); err != nil {
		t.Fatal(err)
	}

	if _, err := offers.OfferTripWithChain(context.Background(), newTripID, "driver-1", 0, OfferChain{AfterTripID: currentTripID}); !errors.Is(err, ErrNotOfferableAfterTrip) {
		t.Fatalf("chained without support: %v", err)
	}

	if len(store.created) != 1 {
		t.Fatalf("calls %+v", store.created)
	}
}

func TestAnUnlikelyRoadTimeIsDropped(t *testing.T) {
	for _, eta := range []time.Duration{-time.Second, MaxOfferPickupETA + time.Second} {
		store := &chainedFakeStore{}
		offers, _ := As[chainedOffers](WithTripOffers(&offersFakeBase{}, store))

		if _, err := offers.OfferTripWithChain(context.Background(), newTripID, "driver-1", 0, OfferChain{PickupETA: eta}); err != nil {
			t.Fatal(err)
		}

		if store.chains[0].PickupETA != 0 {
			t.Errorf("%v kept as %v", eta, store.chains[0].PickupETA)
		}
	}
}

func TestWaitingForThePreviousTrip(t *testing.T) {
	now := time.Now()

	cases := map[string]struct {
		trip Trip
		want bool
	}{
		"an ordinary trip":                   {Trip{HeadingAt: &now}, false},
		"a next trip not set off for":        {Trip{AfterTripID: currentTripID}, true},
		"a next trip the driver set off for": {Trip{AfterTripID: currentTripID, HeadingAt: &now}, false},
		"cancelled while still waiting":      {Trip{AfterTripID: currentTripID, Status: StatusCancelled}, true},
	}

	for name, c := range cases {
		if got := c.trip.WaitingForPreviousTrip(); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
