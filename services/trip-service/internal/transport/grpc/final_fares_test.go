package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type fakeFinalFares struct {
	totals map[string]trip.FinalFare
	err    error
	asked  [][]string
}

func (f *fakeFinalFares) Totals(_ context.Context, ids []string) (map[string]trip.FinalFare, error) {
	f.asked = append(f.asked, ids)

	return f.totals, f.err
}

func quietHandler(fares trip.FinalFares) *TripHandler {
	return &TripHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), finalFares: fares}
}

func TestListedTripsCarryTheirFinalFareInOneCall(t *testing.T) {
	fares := &fakeFinalFares{totals: map[string]trip.FinalFare{
		"t1": {Kind: "trip", Total: "3500", CurrencyCode: "IQD"},
		"t2": {Kind: "cancellation", Total: "1000", CurrencyCode: "IQD"},
	}}
	trips := []*tripv1.Trip{{Id: "t1"}, {Id: "t2"}, {Id: "t3"}}

	quietHandler(fares).addFinalFares(context.Background(), trips)

	if len(fares.asked) != 1 || len(fares.asked[0]) != 3 {
		t.Fatalf("asked %v, want one call with the three trips", fares.asked)
	}

	if trips[0].GetFinalFare() != "3500" || trips[0].GetFinalFareKind() != "trip" || trips[0].GetFinalFareCurrencyCode() != "IQD" {
		t.Errorf("t1 = %v", trips[0])
	}

	if trips[1].GetFinalFareKind() != "cancellation" || trips[1].GetFinalFare() != "1000" {
		t.Errorf("t2 = %v", trips[1])
	}

	if trips[2].GetFinalFare() != "" {
		t.Errorf("t3 has no fare, got %q", trips[2].GetFinalFare())
	}
}

func TestTripsAreStillListedWhenPricingDoesNotAnswer(t *testing.T) {
	trips := []*tripv1.Trip{{Id: "t1"}}

	quietHandler(&fakeFinalFares{err: errors.New("down")}).addFinalFares(context.Background(), trips)

	if trips[0].GetFinalFare() != "" {
		t.Errorf("got a fare %q without pricing", trips[0].GetFinalFare())
	}
}

func TestNoTripsMeansNoCall(t *testing.T) {
	fares := &fakeFinalFares{}

	quietHandler(fares).addFinalFares(context.Background(), nil)

	if len(fares.asked) != 0 {
		t.Errorf("asked pricing %d time(s) for no trips", len(fares.asked))
	}
}
