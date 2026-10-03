package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/activity"
)

// tripAt inserts a trip of the driver in a zone, completed or cancelled at at.
func tripAt(t *testing.T, pool *pgxpool.Pool, driverID, zoneID, class, status string, at time.Time) string {
	t.Helper()

	id := uuid.NewString()
	column := "completed_at"
	cancelledBy := ""

	if status == "cancelled" {
		column, cancelledBy = "cancelled_at", "driver"
	}

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO trips (id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
		    dropoff_latitude, dropoff_longitude, vehicle_class, pickup_zone_id, `+column+`, cancelled_by)
		 VALUES ($1, $2, $3, $4, 36.19, 44.01, 36.2, 44.02, $5, $6, $7, NULLIF($8, ''))`,
		id, uuid.NewString(), driverID, status, class, zoneID, at, cancelledBy); err != nil {
		t.Fatal(err)
	}

	return id
}

func offerAt(t *testing.T, pool *pgxpool.Pool, tripID, driverID, status string, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO trip_offers (trip_id, driver_id, status, offered_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		tripID, driverID, status, at, at.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestDriverActivity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewActivityStore(pool)
	service := activity.NewService(store)

	zone, otherZone := uuid.NewString(), uuid.NewString()
	busy, quiet := uuid.NewString(), uuid.NewString()
	// Monday 2026-09-28 in Baghdad (UTC+3).
	day := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	at := func(hourBaghdad int) time.Time { return day.Add(time.Duration(hourBaghdad-3) * time.Hour) }

	// busy: 3 completed in the zone at 8:00, 9:00 and 20:00 local, 1 in another
	// zone, 1 comfort, and one they cancelled; offers: 3 accepted, 1 rejected,
	// 1 left pending past its expiry.
	for _, hour := range []int{8, 9, 20} {
		trip := tripAt(t, pool, busy, zone, "economy", "completed", at(hour))
		offerAt(t, pool, trip, busy, "accepted", at(hour).Add(-10*time.Minute))
	}

	tripAt(t, pool, busy, otherZone, "economy", "completed", at(10))
	tripAt(t, pool, busy, zone, "comfort", "completed", at(11))
	tripAt(t, pool, busy, zone, "economy", "cancelled", at(12))
	offerAt(t, pool, tripAt(t, pool, quiet, zone, "economy", "completed", at(13)), busy, "rejected", at(13))
	offerAt(t, pool, tripAt(t, pool, quiet, zone, "economy", "completed", at(14)), busy, "pending", at(14))
	// Outside the period.
	tripAt(t, pool, busy, zone, "economy", "completed", day.Add(-4*time.Hour))

	all := activity.Scope{From: day.Add(-3 * time.Hour), To: day.Add(21 * time.Hour)}

	got, err := service.Driver(ctx, busy, all)
	if err != nil {
		t.Fatal(err)
	}

	if got.CompletedTrips != 5 || got.OffersAccepted != 3 || got.OffersDeclined != 2 || got.DriverCancellations != 1 {
		t.Errorf("all of the day: %+v", got)
	}

	scoped := all
	scoped.ZoneIDs = []string{zone}
	scoped.VehicleClass = "economy"
	scoped.DailyStart, scoped.DailyEnd, scoped.TimeZone = 7*60, 10*60, "Asia/Baghdad"

	got, err = service.Driver(ctx, busy, scoped)
	if err != nil {
		t.Fatal(err)
	}

	if got.CompletedTrips != 2 || got.OffersAccepted != 2 || got.OffersDeclined != 0 || got.DriverCancellations != 0 {
		t.Errorf("economy in the zone, 7:00-10:00: %+v", got)
	}

	// A window across midnight: 19:00-02:00 keeps the 20:00 trip only.
	night := all
	night.DailyStart, night.DailyEnd, night.TimeZone = 19*60, 2*60, "Asia/Baghdad"

	if got, _ = service.Driver(ctx, busy, night); got.CompletedTrips != 1 {
		t.Errorf("night: %+v", got)
	}

	page, err := service.Drivers(ctx, all, 3, 10, "")
	if err != nil || len(page.Drivers) != 1 || page.Drivers[0].DriverID != busy {
		t.Fatalf("drivers with 3 trips: %+v %v", page, err)
	}

	page, err = service.Drivers(ctx, all, 1, 1, "")
	if err != nil || len(page.Drivers) != 1 || page.NextPageToken == "" {
		t.Fatalf("first page of one: %+v %v", page, err)
	}

	next, err := service.Drivers(ctx, all, 1, 1, page.NextPageToken)
	if err != nil || len(next.Drivers) != 1 || next.Drivers[0].DriverID == page.Drivers[0].DriverID || next.NextPageToken != "" {
		t.Fatalf("second page: %+v %v", next, err)
	}
}
