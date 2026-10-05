package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/ingest"
	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

// testPool applies the Up part of every migration to an empty database.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("ANALYTICS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ANALYTICS_TEST_DATABASE_URL is not set")
	}

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	psql(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)

	files, err := filepath.Glob("../../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}

	sort.Strings(files)

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		psql(strings.SplitN(string(content), "-- +goose Down", 2)[0])
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

type cityZones map[string]string

func (c cityZones) TimeZone(_ context.Context, id string) (string, error) {
	if zone, ok := c[id]; ok {
		return zone, nil
	}

	return "", domain.ErrUnknownCity
}

type feed struct {
	t       *testing.T
	handler *ingest.Handler
}

func (f feed) send(eventID, eventType, at string, payload any) {
	f.t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}

	envelope, err := json.Marshal(map[string]any{
		"event_id": eventID, "event_type": eventType, "occurred_at": at, "payload": json.RawMessage(body),
	})
	if err != nil {
		f.t.Fatal(err)
	}

	if err := f.handler.Dispatch(context.Background(), eventType, envelope); err != nil {
		f.t.Fatalf("%s %s: %v", eventType, eventID, err)
	}
}

func TestReportsFromEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := feed{t: t, handler: ingest.NewHandler(postgres.NewWriter(pool), logger)}

	erbil, other := uuid.NewString(), uuid.NewString()
	zone := uuid.NewString()
	rider, driver := uuid.NewString(), uuid.NewString()
	trips := []string{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()}

	request := func(id, trip, at, city, class string) {
		f.send(id, "trip.requested", at, map[string]string{
			"trip_id": trip, "rider_id": rider, "city_id": city, "zone_id": zone,
			"vehicle_class": class, "payment_method": "cash", "scheduled": "false",
		})
	}

	// Trip 0: 23:30 UTC on 1 Oct = 2 Oct in Baghdad; completed, fare and
	// commission. The settlement comes before the fare (another stream).
	request("e1", trips[0], "2026-10-01T22:30:00Z", erbil, "economy")
	f.send("e2", "trip.accepted", "2026-10-01T22:31:00Z", map[string]string{"trip_id": trips[0], "driver_id": driver})
	f.send("e3", "trip.started", "2026-10-01T22:40:00Z", map[string]string{"trip_id": trips[0]})
	f.send("e4", "trip.completed", "2026-10-01T23:00:00Z", map[string]string{"trip_id": trips[0], "rider_id": rider, "driver_id": driver})
	f.send("e5", "trip.settled", "2026-10-01T23:00:02Z", map[string]string{"trip_id": trips[0], "driver_id": driver, "commission_amount": "1500.000"})
	f.send("e6", "fare.calculated", "2026-10-01T23:00:01Z", map[string]any{"trip_id": trips[0], "currency_code": "IQD", "total": 10000, "kind": "trip"})
	f.send("e6", "fare.calculated", "2026-10-01T23:00:01Z", map[string]any{"trip_id": trips[0], "currency_code": "IQD", "total": 99999, "kind": "trip"})

	// Trip 1: cancelled by the driver after arriving (rider no-show), fee.
	request("e7", trips[1], "2026-10-02T09:00:00Z", erbil, "comfort")
	f.send("e8", "trip.accepted", "2026-10-02T09:01:00Z", map[string]string{"trip_id": trips[1], "driver_id": driver})
	f.send("e9", "trip.driver_arrived", "2026-10-02T09:05:00Z", map[string]string{"trip_id": trips[1]})
	f.send("e10", "trip.cancelled", "2026-10-02T09:12:00Z", map[string]string{
		"trip_id": trips[1], "cancelled_by": "driver", "rider_no_show": "true", "from_status": "accepted", "driver_arrived": "true",
	})
	f.send("e11", "fare.calculated", "2026-10-02T09:12:01Z", map[string]any{"trip_id": trips[1], "currency_code": "IQD", "total": "2000", "kind": "no_show"})

	// Trip 2: cancelled by the rider while still requested; an old event
	// without from_status.
	request("e12", trips[2], "2026-10-02T10:00:00Z", erbil, "economy")
	f.send("e13", "trip.cancelled", "2026-10-02T10:01:00Z", map[string]string{"trip_id": trips[2], "reason": "changed mind", "cancelled_by": "rider"})

	// Trip 3: another city; trip 4: outside the range (3 Oct in Baghdad).
	request("e14", trips[3], "2026-10-02T11:00:00Z", other, "economy")
	request("e15", trips[4], "2026-10-02T21:30:00Z", erbil, "economy")

	// Unusable events are skipped, not retried forever.
	f.send("e16", "trip.accepted", "2026-10-02T11:00:00Z", map[string]string{"driver_id": driver})
	f.send("e17", "trip.settled", "2026-10-02T11:00:00Z", map[string]string{"trip_id": trips[3], "commission_amount": "lots"})
	if err := f.handler.Dispatch(ctx, "trip.requested", []byte("not json")); err != nil {
		t.Fatalf("bad JSON must be skipped: %v", err)
	}

	baghdad := cityZones{erbil: "Asia/Baghdad", other: "Asia/Baghdad"}
	service := query.NewService(postgres.NewReader(pool), baghdad, time.UTC)
	rng := domain.DateRange{From: "2026-10-02", To: "2026-10-02"}
	inErbil := domain.Scope{CityID: erbil}

	days, totals, w, err := service.TripFunnel(ctx, rng, inErbil)
	if err != nil {
		t.Fatal(err)
	}

	if w.Location.String() != "Asia/Baghdad" || len(days) != 1 {
		t.Fatalf("window %v, days %d", w.Location, len(days))
	}

	if totals.RequestedCount != 3 || totals.AcceptedCount != 2 || totals.StartedCount != 1 || totals.CompletedCount != 1 || totals.CancelledCount != 2 {
		t.Fatalf("funnel in Erbil on 2 Oct (Baghdad): %+v", totals)
	}

	// The platform clock (UTC) puts trip 0 on 1 Oct and trip 4 on 2 Oct.
	_, utcTotals, _, err := service.TripFunnel(ctx, domain.DateRange{From: "2026-10-01", To: "2026-10-02"}, domain.Scope{})
	if err != nil {
		t.Fatal(err)
	}

	if utcTotals.RequestedCount != 5 {
		t.Fatalf("all trips over two UTC days: %+v", utcTotals)
	}

	_, comfort, _, err := service.TripFunnel(ctx, rng, domain.Scope{CityID: erbil, ZoneID: zone, VehicleClass: "comfort"})
	if err != nil || comfort.RequestedCount != 1 {
		t.Fatalf("comfort only: %+v %v", comfort, err)
	}

	cancellations, _, err := service.Cancellations(ctx, rng, inErbil)
	if err != nil {
		t.Fatal(err)
	}

	stages := map[domain.CancelStage]int64{}
	for _, s := range cancellations.ByStage {
		stages[s.Stage] = s.Count
	}

	by := map[string]int64{}
	for _, b := range cancellations.ByCancelledBy {
		by[b.CancelledBy] = b.Count
	}

	if cancellations.TotalTrips != 3 || cancellations.TotalCancellations != 2 || cancellations.RiderNoShows != 1 ||
		stages[domain.CancelStageArrived] != 1 || stages[domain.CancelStageRequested] != 1 ||
		by["driver"] != 1 || by["rider"] != 1 || cancellations.CancellationRatePct.StringFixed(2) != "66.67" {
		t.Fatalf("cancellations %+v", cancellations)
	}

	revenueDays, summary, _, err := service.Revenue(ctx, rng, inErbil)
	if err != nil {
		t.Fatal(err)
	}

	if len(revenueDays) != 1 || summary.Currency != "IQD" || summary.GrossFareTotal.String() != "10000" ||
		summary.TotalTrips != 1 || summary.FeeTotal.String() != "2000" || summary.TotalFees != 1 ||
		summary.CommissionTotal.String() != "1500" {
		t.Fatalf("revenue %+v %+v", revenueDays, summary)
	}

	if _, _, err := service.Cancellations(ctx, rng, domain.Scope{CityID: uuid.NewString()}); err == nil {
		t.Fatal("an unknown city must be refused")
	}

	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM processed_events`).Scan(&stored); err != nil || stored != 17 {
		t.Fatalf("processed events %d %v (each id once, the bad JSON not at all)", stored, err)
	}
}

func TestRetention(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := feed{t: t, handler: ingest.NewHandler(postgres.NewWriter(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))}

	thisMonday := query.MondayOf(time.Now().UTC().Truncate(24 * time.Hour))
	weekAgo := thisMonday.AddDate(0, 0, -7).Add(10 * time.Hour).Format(time.RFC3339)
	thisWeek := thisMonday.Add(10 * time.Hour).Format(time.RFC3339)

	a, b := uuid.NewString(), uuid.NewString()
	f.send("r1", "rider.created", weekAgo, map[string]string{"rider_id": a, "display_name": "Sara"})
	f.send("r2", "rider.created", weekAgo, map[string]string{"rider_id": b, "display_name": "Ali"})
	f.send("r3", "trip.requested", weekAgo, map[string]string{"trip_id": uuid.NewString(), "rider_id": a})
	f.send("r4", "trip.requested", weekAgo, map[string]string{"trip_id": uuid.NewString(), "rider_id": b})
	f.send("r5", "trip.requested", thisWeek, map[string]string{"trip_id": uuid.NewString(), "rider_id": a})

	d := uuid.NewString()
	f.send("d1", "driver.created", weekAgo, map[string]string{"driver_id": d})
	f.send("d2", "driver.approved", thisWeek, map[string]string{"driver_id": d})
	f.send("d3", "trip.accepted", thisWeek, map[string]string{"trip_id": uuid.NewString(), "driver_id": d})

	service := query.NewService(postgres.NewReader(pool), cityZones{}, time.UTC)

	riders, _, err := service.RiderRetention(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}

	if len(riders) != 1 || riders[0].CohortSize != 2 || len(riders[0].RetentionPercentByWeek) != 2 ||
		riders[0].RetentionPercentByWeek[0].StringFixed(2) != "100.00" || riders[0].RetentionPercentByWeek[1].StringFixed(2) != "50.00" {
		t.Fatalf("riders %+v", riders)
	}

	drivers, _, err := service.DriverRetention(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}

	// A driver's cohort is the week they were approved.
	if len(drivers) != 1 || drivers[0].CohortWeek != thisMonday.Format(time.DateOnly) || drivers[0].CohortSize != 1 ||
		drivers[0].RetentionPercentByWeek[0].StringFixed(2) != "100.00" {
		t.Fatalf("drivers %+v", drivers)
	}

	var names int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND column_name = 'payload'`).Scan(&names); err != nil || names != 0 {
		t.Fatalf("no table may keep event payloads: %d %v", names, err)
	}
}
