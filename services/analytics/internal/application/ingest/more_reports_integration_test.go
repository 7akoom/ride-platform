package ingest_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/ingest"
	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

type fixedDrivers []domain.DriverSupply

func (f fixedDrivers) Supply(context.Context) ([]domain.DriverSupply, error) { return f, nil }

func TestServiceOffersRatingsAndLive(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := feed{t: t, handler: ingest.NewHandler(postgres.NewWriter(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))}

	city := uuid.NewString()
	driverA, driverB := uuid.NewString(), uuid.NewString()
	done, live, waiting := uuid.NewString(), uuid.NewString(), uuid.NewString()
	n := 0
	id := func() string {
		n++
		return "m" + time.Now().Format("150405.000000") + "-" + string(rune('a'+n%26)) + uuid.NewString()[:4]
	}

	now := time.Now().UTC().Truncate(time.Second)
	at := func(minutes int) string { return now.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339) }
	request := func(trip string, minutes int) {
		f.send(id(), "trip.requested", at(minutes), map[string]string{"trip_id": trip, "rider_id": uuid.NewString(), "city_id": city, "vehicle_class": "economy"})
	}

	// done: driver A turns it down, driver B takes it 60 s after the
	// request, arrives 5 min later, drives 20 min; discount and surge.
	request(done, -40)
	f.send(id(), "trip.offered", at(-40), map[string]string{"trip_id": done, "driver_id": driverA, "offered_at": at(-40), "expires_at": at(-39)})
	f.send(id(), "trip.offer_rejected", at(-40), map[string]string{"trip_id": done, "driver_id": driverA})
	f.send(id(), "trip.offered", at(-40), map[string]string{"trip_id": done, "driver_id": driverB, "offered_at": at(-40), "expires_at": at(-39)})
	f.send(id(), "trip.accepted", now.Add(-39*time.Minute).Format(time.RFC3339), map[string]string{"trip_id": done, "driver_id": driverB})
	f.send(id(), "trip.driver_arrived", at(-34), map[string]string{"trip_id": done})
	f.send(id(), "trip.started", at(-30), map[string]string{"trip_id": done})
	f.send(id(), "trip.completed", at(-10), map[string]string{"trip_id": done})
	f.send(id(), "fare.calculated", at(-10), map[string]any{"trip_id": done, "currency_code": "IQD", "total": 9000, "kind": "trip", "discount_amount": 1000, "surge_amount": 1500})
	f.send(id(), "trip.rated", at(-9), map[string]string{"trip_id": done, "rated_by": "rider", "stars": "4"})
	f.send(id(), "trip.rated", at(-9), map[string]string{"trip_id": done, "rated_by": "driver", "stars": "5"})
	f.send(id(), "trip.rated", at(-9), map[string]string{"trip_id": done, "rated_by": "nobody", "stars": "9"})

	// live: accepted and on the way; waiting: an open offer no one answered.
	request(live, -5)
	f.send(id(), "trip.accepted", at(-4), map[string]string{"trip_id": live, "driver_id": driverA})
	request(waiting, -1)
	f.send(id(), "trip.offered", at(-1), map[string]string{"trip_id": waiting, "driver_id": driverA, "offered_at": at(-1), "expires_at": at(5)})

	// An offer that ran out yesterday-ish: an earlier trip.
	old := uuid.NewString()
	request(old, -50)
	f.send(id(), "trip.offered", at(-50), map[string]string{"trip_id": old, "driver_id": driverB, "offered_at": at(-50), "expires_at": at(-49)})

	service := query.NewService(postgres.NewReader(pool), cityZones{city: "UTC"}, time.UTC).
		WithDrivers(fixedDrivers{{Status: "active", Availability: "available", VehicleClass: "economy", Drivers: 2}})
	today := domain.DateRange{From: now.AddDate(0, 0, -1).Format(time.DateOnly), To: now.Format(time.DateOnly)}
	scope := domain.Scope{CityID: city}

	_, levels, _, err := service.ServiceLevels(ctx, today, scope)
	if err != nil {
		t.Fatal(err)
	}

	if levels.Requested != 4 || levels.Completed != 1 || levels.Match.Count != 2 || levels.Pickup.Count != 1 ||
		levels.Pickup.Average != 300 || levels.Ride.Median != 1200 || levels.Match.P90 < 60 {
		t.Fatalf("service levels %+v", levels)
	}

	_, offers, _, err := service.DriverOffers(ctx, today, scope)
	if err != nil {
		t.Fatal(err)
	}

	if offers.Offered != 4 || offers.Accepted != 1 || offers.Rejected != 1 || offers.Expired != 1 || offers.Pending != 1 ||
		query.AcceptanceRate(offers).StringFixed(2) != "33.33" {
		t.Fatalf("offers %+v", offers)
	}

	ratings, _, err := service.Ratings(ctx, today, scope)
	if err != nil {
		t.Fatal(err)
	}

	if ratings.CompletedTrips != 1 || ratings.Drivers.Count != 1 || ratings.Drivers.ByStars[3] != 1 ||
		ratings.Drivers.Average.StringFixed(2) != "4.00" || ratings.Riders.ByStars[4] != 1 {
		t.Fatalf("ratings %+v", ratings)
	}

	_, revenue, _, err := service.Revenue(ctx, today, scope)
	if err != nil || revenue.DiscountTotal.String() != "1000" || revenue.SurgeTotal.String() != "1500" {
		t.Fatalf("revenue %+v %v", revenue, err)
	}

	overview, _, err := service.LiveOverview(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}

	if overview.Trips.Waiting != 2 || overview.Trips.OnTheWay != 1 || overview.Trips.InProgress != 0 || overview.Available != 2 {
		t.Fatalf("live %+v", overview)
	}

	if _, _, err := query.NewService(postgres.NewReader(pool), cityZones{}, time.UTC).LiveOverview(ctx, domain.Scope{}); err == nil {
		t.Fatal("no driver-service must be an error")
	}
}
