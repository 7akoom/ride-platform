package trip

import (
	"context"
	"errors"
	"testing"
	"time"
)

type offersFakeBase struct{ Service }

type offersFakeStore struct {
	offer Offer
	trip  Trip
	err   error

	created  []offerCall
	accepted []offerCall
	rejected []offerCall
	pending  []string
}

type offerCall struct {
	trip   string
	driver string
	ttl    time.Duration
}

func (s *offersFakeStore) CreateOffer(_ context.Context, tripID string, driverID string, ttl time.Duration) (Offer, error) {
	s.created = append(s.created, offerCall{tripID, driverID, ttl})

	return s.offer, s.err
}

func (s *offersFakeStore) FindPendingOffer(_ context.Context, driverID string) (Offer, error) {
	s.pending = append(s.pending, driverID)

	return s.offer, s.err
}

func (s *offersFakeStore) AcceptOffer(_ context.Context, tripID string, driverID string) (Trip, error) {
	s.accepted = append(s.accepted, offerCall{trip: tripID, driver: driverID})

	return s.trip, s.err
}

func (s *offersFakeStore) RejectOffer(_ context.Context, tripID string, driverID string) error {
	s.rejected = append(s.rejected, offerCall{trip: tripID, driver: driverID})

	return s.err
}

type offersReader interface {
	OfferTrip(ctx context.Context, tripID string, driverID string, ttl time.Duration) (Offer, error)
	GetPendingOffer(ctx context.Context, driverID string) (Offer, error)
	AcceptOffer(ctx context.Context, tripID string, driverID string) (Trip, error)
	RejectOffer(ctx context.Context, tripID string, driverID string) error
}

func newOffersUnderTest(t *testing.T) (offersReader, *offersFakeStore) {
	t.Helper()

	store := &offersFakeStore{offer: Offer{TripID: "trip-1", DriverID: "driver-a"}, trip: Trip{ID: "trip-1", Status: StatusAccepted}}

	reader, ok := As[offersReader](WithTripOffers(&offersFakeBase{}, store))
	if !ok {
		t.Fatal("the decorated service must expose the offer methods")
	}

	return reader, store
}

func TestOfferTripPassesTheIdsAndAClampedTTLToTheStore(t *testing.T) {
	cases := []struct {
		asked time.Duration
		want  time.Duration
	}{
		{0, DefaultOfferTTL},
		{-time.Second, DefaultOfferTTL},
		{time.Second, MinOfferTTL},
		{MinOfferTTL, MinOfferTTL},
		{20 * time.Second, 20 * time.Second},
		{MaxOfferTTL, MaxOfferTTL},
		{time.Hour, MaxOfferTTL},
	}

	for _, tc := range cases {
		reader, store := newOffersUnderTest(t)

		got, err := reader.OfferTrip(context.Background(), " trip-1 ", " driver-a ", tc.asked)
		if err != nil || got.TripID != "trip-1" {
			t.Fatalf("ttl %v: %+v, %v", tc.asked, got, err)
		}

		if len(store.created) != 1 || store.created[0] != (offerCall{"trip-1", "driver-a", tc.want}) {
			t.Errorf("ttl %v: the store was asked %+v, expected ttl %v and trimmed ids", tc.asked, store.created, tc.want)
		}
	}
}

func TestEveryOfferCallNeedsBothIdsAndNeverReachesTheStoreWithout(t *testing.T) {
	reader, store := newOffersUnderTest(t)
	ctx := context.Background()

	if _, err := reader.OfferTrip(ctx, "", "driver-a", 0); !errors.Is(err, ErrTripIDRequired) {
		t.Errorf("OfferTrip without a trip: %v", err)
	}

	if _, err := reader.OfferTrip(ctx, "trip-1", "  ", 0); !errors.Is(err, ErrDriverIDRequired) {
		t.Errorf("OfferTrip without a driver: %v", err)
	}

	if _, err := reader.GetPendingOffer(ctx, ""); !errors.Is(err, ErrDriverIDRequired) {
		t.Errorf("GetPendingOffer without a driver: %v", err)
	}

	if _, err := reader.AcceptOffer(ctx, "", "driver-a"); !errors.Is(err, ErrTripIDRequired) {
		t.Errorf("AcceptOffer without a trip: %v", err)
	}

	if _, err := reader.AcceptOffer(ctx, "trip-1", ""); !errors.Is(err, ErrDriverIDRequired) {
		t.Errorf("AcceptOffer without a driver: %v", err)
	}

	if err := reader.RejectOffer(ctx, "", "driver-a"); !errors.Is(err, ErrTripIDRequired) {
		t.Errorf("RejectOffer without a trip: %v", err)
	}

	if err := reader.RejectOffer(ctx, "trip-1", ""); !errors.Is(err, ErrDriverIDRequired) {
		t.Errorf("RejectOffer without a driver: %v", err)
	}

	if len(store.created)+len(store.pending)+len(store.accepted)+len(store.rejected) != 0 {
		t.Errorf("the store was reached with an invalid request: %+v", store)
	}
}

func TestTheDriversOffersAreReadAcceptedAndRejectedThroughTheStore(t *testing.T) {
	reader, store := newOffersUnderTest(t)
	ctx := context.Background()

	if offer, err := reader.GetPendingOffer(ctx, " driver-a "); err != nil || offer.TripID != "trip-1" || store.pending[0] != "driver-a" {
		t.Errorf("GetPendingOffer: %+v, %v, asked %v", offer, err, store.pending)
	}

	if got, err := reader.AcceptOffer(ctx, "trip-1", "driver-a"); err != nil || got.Status != StatusAccepted || store.accepted[0] != (offerCall{trip: "trip-1", driver: "driver-a"}) {
		t.Errorf("AcceptOffer: %+v, %v, asked %+v", got, err, store.accepted)
	}

	if err := reader.RejectOffer(ctx, "trip-1", "driver-a"); err != nil || store.rejected[0] != (offerCall{trip: "trip-1", driver: "driver-a"}) {
		t.Errorf("RejectOffer: %v, asked %+v", err, store.rejected)
	}
}

func TestTheStoresRefusalsPassThroughUnchanged(t *testing.T) {
	for _, refusal := range []error{
		ErrTripNotOfferable, ErrOfferInProgress, ErrAlreadyOffered, ErrDriverHasPendingOffer,
		ErrOfferNotFound, ErrOfferExpired, ErrDriverHasActiveTrip, ErrInvalidTransition, ErrTripNotFound,
		errors.New("database down"),
	} {
		reader, store := newOffersUnderTest(t)
		store.err = refusal
		ctx := context.Background()

		if _, err := reader.OfferTrip(ctx, "trip-1", "driver-a", 0); !errors.Is(err, refusal) {
			t.Errorf("OfferTrip: expected %v, got %v", refusal, err)
		}

		if _, err := reader.GetPendingOffer(ctx, "driver-a"); !errors.Is(err, refusal) {
			t.Errorf("GetPendingOffer: expected %v, got %v", refusal, err)
		}

		if _, err := reader.AcceptOffer(ctx, "trip-1", "driver-a"); !errors.Is(err, refusal) {
			t.Errorf("AcceptOffer: expected %v, got %v", refusal, err)
		}

		if err := reader.RejectOffer(ctx, "trip-1", "driver-a"); !errors.Is(err, refusal) {
			t.Errorf("RejectOffer: expected %v, got %v", refusal, err)
		}
	}
}

func TestTheOfferDecoratorServesEveryOtherMethodFromTheBase(t *testing.T) {
	base := &trackingFakeBase{trips: map[string]Trip{"trip-1": {ID: "trip-1"}}}
	decorated := WithTripOffers(base, &offersFakeStore{})

	if got, err := decorated.GetTrip(context.Background(), "trip-1"); err != nil || got.ID != "trip-1" || base.reads != 1 {
		t.Errorf("GetTrip must reach the base service: %+v, %v, reads=%d", got, err, base.reads)
	}
}

func TestTheOfferDecoratorRequiresBothParts(t *testing.T) {
	for name, build := range map[string]func(){
		"no base service": func() { WithTripOffers(nil, &offersFakeStore{}) },
		"no store":        func() { WithTripOffers(&offersFakeBase{}, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected a panic", name)
				}
			}()

			build()
		}()
	}
}

func TestOffersHistoryAndTrackingAreAllReachableInAnyOrder(t *testing.T) {
	type tracker interface {
		GetDriverLocation(ctx context.Context, tripID string) (DriverLocation, error)
	}

	base := &trackingFakeBase{}
	locator := &trackingFakeLocator{}
	history := &historyFakeStore{}
	offers := &offersFakeStore{}

	stacks := map[string]Service{
		"offers outermost": WithTripOffers(WithTripHistory(WithDriverTracking(base, locator), history), offers),
		"offers innermost": WithDriverTracking(WithTripHistory(WithTripOffers(base, offers), history), locator),
		"offers between":   WithTripHistory(WithTripOffers(WithDriverTracking(base, locator), offers), history),
	}

	for name, stack := range stacks {
		if _, ok := As[tracker](stack); !ok {
			t.Errorf("%s: driver tracking is not reachable", name)
		}

		if _, ok := As[historyReader](stack); !ok {
			t.Errorf("%s: the history is not reachable", name)
		}

		if _, ok := As[offersReader](stack); !ok {
			t.Errorf("%s: the offers are not reachable", name)
		}
	}
}
