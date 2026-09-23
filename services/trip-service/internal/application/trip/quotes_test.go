package trip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const quoteID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"

type fakeQuoteBook struct {
	quote    trip.ClaimedQuote
	err      error
	claimed  []string
	released []string
}

func (b *fakeQuoteBook) Claim(_ context.Context, quoteID, riderID, tripID string) (trip.ClaimedQuote, error) {
	b.claimed = append(b.claimed, quoteID+" "+riderID+" "+tripID)

	return b.quote, b.err
}

func (b *fakeQuoteBook) Release(_ context.Context, quoteID, tripID string) error {
	b.released = append(b.released, quoteID+" "+tripID)

	return nil
}

func quoteRig() (trip.Service, *fakeRepository, *fakeQuoteBook) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	book := &fakeQuoteBook{quote: trip.ClaimedQuote{
		ID:           quoteID,
		VehicleClass: "comfort",
		// A few metres from validRequestInput's points.
		Pickup:       trip.Coordinates{Latitude: 36.19002, Longitude: 44.01002},
		Dropoff:      trip.Coordinates{Latitude: 36.20, Longitude: 44.02},
		Total:        "7250",
		CurrencyCode: "IQD",
	}}

	svc := trip.NewService(repo, &fakeIDGenerator{id: "new-trip-id"}, &fakeZoneChecker{served: true}, trip.WithQuotes(book))

	return svc, repo, book
}

func quotedInput() trip.RequestTripInput {
	input := validRequestInput()
	input.QuoteID = quoteID

	return input
}

func TestATripRequestedWithAQuoteKeepsItsPriceAndClass(t *testing.T) {
	svc, repo, book := quoteRig()

	if _, err := svc.RequestTrip(context.Background(), quotedInput()); err != nil {
		t.Fatal(err)
	}

	if len(book.claimed) != 1 || book.claimed[0] != quoteID+" rider-1 new-trip-id" {
		t.Fatalf("claimed %v", book.claimed)
	}

	got := repo.createCalls[0]
	if got.QuoteID != quoteID || got.QuotedFare != "7250" || got.CurrencyCode != "IQD" || got.VehicleClass != "comfort" {
		t.Fatalf("created %+v", got)
	}

	// The rider's own points are kept (they are the quoted ones).
	if got.Pickup.Latitude != 36.19 {
		t.Fatalf("pickup %+v", got.Pickup)
	}
}

func TestAQuoteMustMatchTheTrip(t *testing.T) {
	cases := map[string]func(*trip.RequestTripInput){
		"another pickup":  func(i *trip.RequestTripInput) { i.PickupLat = 36.195 },
		"another dropoff": func(i *trip.RequestTripInput) { i.DropoffLng = 44.03 },
		"another class":   func(i *trip.RequestTripInput) { i.VehicleClass = "economy" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo, book := quoteRig()
			input := quotedInput()
			mutate(&input)

			if _, err := svc.RequestTrip(context.Background(), input); !errors.Is(err, trip.ErrQuoteMismatch) {
				t.Fatalf("got %v", err)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("no trip may be created")
			}

			if len(book.released) != 1 || book.released[0] != quoteID+" new-trip-id" {
				t.Fatalf("the quote must be released: %v", book.released)
			}
		})
	}
}

func TestTheQuotedClassMayBeNamed(t *testing.T) {
	svc, repo, _ := quoteRig()
	input := quotedInput()
	input.VehicleClass = " Comfort "

	if _, err := svc.RequestTrip(context.Background(), input); err != nil || repo.createCalls[0].VehicleClass != "comfort" {
		t.Fatalf("err %v", err)
	}
}

func TestAQuoteThatCannotBeClaimedCreatesNoTrip(t *testing.T) {
	for _, claimErr := range []error{trip.ErrQuoteNotFound, trip.ErrQuoteNotUsable, trip.ErrUpstreamUnavailable} {
		svc, repo, book := quoteRig()
		book.err = claimErr

		if _, err := svc.RequestTrip(context.Background(), quotedInput()); !errors.Is(err, claimErr) {
			t.Fatalf("got %v, want %v", err, claimErr)
		}

		if len(repo.createCalls) != 0 || len(book.released) != 0 {
			t.Fatalf("created %d, released %v", len(repo.createCalls), book.released)
		}
	}
}

func TestAFailedCreateReleasesTheQuote(t *testing.T) {
	svc, repo, book := quoteRig()
	repo.createErr = errors.New("database down")

	if _, err := svc.RequestTrip(context.Background(), quotedInput()); err == nil {
		t.Fatal("expected the failure")
	}

	if len(book.released) != 1 {
		t.Fatalf("released %v", book.released)
	}
}

func TestQuotesAreCheckedBeforeAnythingIsClaimed(t *testing.T) {
	// No quote book: a quote is refused.
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	if _, err := newService(repo).RequestTrip(context.Background(), quotedInput()); !errors.Is(err, trip.ErrQuotesUnavailable) {
		t.Fatalf("no book: %v", err)
	}

	// A malformed id is never sent to pricing.
	svc, _, book := quoteRig()
	input := quotedInput()
	input.QuoteID = "quote-1"

	if _, err := svc.RequestTrip(context.Background(), input); !errors.Is(err, trip.ErrQuoteNotFound) || len(book.claimed) != 0 {
		t.Fatalf("malformed: %v, claimed %v", err, book.claimed)
	}

	// A rider with an active trip does not spend a quote.
	svc, repo, book = quoteRig()
	repo.findActiveByRiderIDErr = nil

	if _, err := svc.RequestTrip(context.Background(), quotedInput()); !errors.Is(err, trip.ErrRiderHasActiveTrip) || len(book.claimed) != 0 {
		t.Fatalf("active trip: %v, claimed %v", err, book.claimed)
	}
}
