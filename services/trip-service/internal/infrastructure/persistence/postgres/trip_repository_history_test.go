package postgres

import (
	"strings"
	"testing"
)

func normalized(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

func TestHistoryQueryFirstPage(t *testing.T) {
	got := normalized(historyQuery("rider_id", false))

	for _, want := range []string{
		"FROM trips WHERE rider_id = $1",
		"ORDER BY requested_at DESC, id DESC",
		"LIMIT $2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the first-page query lacks %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "$3") || strings.Contains(got, "(requested_at, id) <") {
		t.Errorf("the first page must not have a cursor condition:\n%s", got)
	}
}

func TestHistoryQueryContinuesAfterTheCursorTripOfTheSameOwner(t *testing.T) {
	for _, column := range []string{"rider_id", "driver_id"} {
		got := normalized(historyQuery(column, true))

		for _, want := range []string{
			"WHERE " + column + " = $1",
			"AND (requested_at, id) < ( SELECT requested_at, id FROM trips WHERE id = $2 AND " + column + " = $1 )",
			"ORDER BY requested_at DESC, id DESC",
			"LIMIT $3",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the cursor query lacks %q:\n%s", column, want, got)
			}
		}
	}
}

func TestHistoryQueryReadsTheColumnsScanTripExpects(t *testing.T) {
	got := normalized(historyQuery("driver_id", false))

	if !strings.Contains(got, normalized(historyColumns)) {
		t.Errorf("the query must select historyColumns:\n%s", got)
	}

	// The same list every other trip query selects, so scanTrip reads them all
	// the same way.
	if historyColumns != tripColumns {
		t.Error("historyColumns must be tripColumns")
	}

	want := "id, rider_id, driver_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude, " +
		"cancellation_reason, vehicle_class, payment_method, requested_at, accepted_at, started_at, completed_at, " +
		"cancelled_at, created_at, updated_at, pickup_address, dropoff_address, pickup_details, pickup_note, " +
		"COALESCE(pickup_photo_media_id::text, ''), COALESCE(quote_id::text, ''), COALESCE(quoted_fare::text, ''), " +
		"COALESCE(currency_code, '')"
	if normalized(historyColumns) != want {
		t.Errorf("historyColumns changed:\n%s", normalized(historyColumns))
	}
}

func TestHistoryQueryRefusesAnyOtherColumn(t *testing.T) {
	for _, column := range []string{"", "id", "rider_id; DROP TABLE trips", "status"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: expected a panic", column)
				}
			}()

			historyQuery(column, false)
		}()
	}
}
