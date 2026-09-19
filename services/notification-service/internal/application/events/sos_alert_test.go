package events

import (
	"strings"
	"testing"
	"time"
)

var sosTime = time.Date(2026, 9, 19, 13, 8, 0, 0, time.UTC)

func sosSamplePayload() tripSOSTriggeredPayload {
	return tripSOSTriggeredPayload{
		TripID:      "995e43e6-7114-4907-8277-f09eef670bdc",
		AlertID:     "3f2a91bc-1111-2222-3333-444455556666",
		TriggeredBy: "rider",
		Latitude:    "36.191234",
		Longitude:   "44.012345",
	}
}

func TestBuildSOSAlertPrefersTheExactPlaceSOSWasPressed(t *testing.T) {
	trip := TripInfo{PickupLatitude: 36.5, PickupLongitude: 44.5}

	alert := buildSOSAlert(sosSamplePayload(), trip, "Test Driver", sosTime)

	if !alert.HasLocation || alert.LocationSource != LocationSourceSOSPress {
		t.Fatalf("expected the press location, got %+v", alert)
	}

	if alert.Latitude != 36.191234 || alert.Longitude != 44.012345 {
		t.Fatalf("wrong coordinates: %v, %v", alert.Latitude, alert.Longitude)
	}

	if alert.DriverName != "Test Driver" || alert.TriggeredBy != "rider" || !alert.TriggeredAt.Equal(sosTime) {
		t.Fatalf("unexpected alert: %+v", alert)
	}
}

func TestBuildSOSAlertFallsBackToThePickupWhenThePressLocationIsUnusable(t *testing.T) {
	trip := TripInfo{PickupLatitude: 36.5, PickupLongitude: 44.5}

	cases := map[string]tripSOSTriggeredPayload{
		"missing":      {TripID: "t", AlertID: "a", TriggeredBy: "driver"},
		"not a number": {TripID: "t", AlertID: "a", TriggeredBy: "driver", Latitude: "abc", Longitude: "44"},
		"out of range": {TripID: "t", AlertID: "a", TriggeredBy: "driver", Latitude: "91", Longitude: "44"},
		"exact zero":   {TripID: "t", AlertID: "a", TriggeredBy: "driver", Latitude: "0", Longitude: "0"},
	}

	for name, payload := range cases {
		alert := buildSOSAlert(payload, trip, "", sosTime)

		if !alert.HasLocation || alert.LocationSource != LocationSourcePickup || alert.Latitude != 36.5 {
			t.Fatalf("%s: expected the pickup fallback, got %+v", name, alert)
		}
	}
}

func TestBuildSOSAlertWithNoLocationAtAll(t *testing.T) {
	payload := tripSOSTriggeredPayload{TripID: "t", AlertID: "a", TriggeredBy: "rider"}

	alert := buildSOSAlert(payload, TripInfo{}, "", sosTime)

	if alert.HasLocation || alert.MapURL() != "" {
		t.Fatalf("expected no location, got %+v", alert)
	}

	if !strings.Contains(alert.Text(), "No location available.") {
		t.Fatalf("the text must say there is no location: %q", alert.Text())
	}
}

func TestSOSAlertTextTellsTheOperatorEverythingNeeded(t *testing.T) {
	alert := buildSOSAlert(sosSamplePayload(), TripInfo{}, "Test Driver", sosTime)

	text := alert.Text()

	for _, want := range []string{
		"SOS ALERT",
		"the rider pressed SOS",
		"995e43e6-7114-4907-8277-f09eef670bdc",
		"Driver: Test Driver.",
		"where SOS was pressed",
		"https://www.openstreetmap.org/?mlat=36.191234&mlon=44.012345",
		"Alert 3f2a91bc at 2026-09-19 13:08 UTC.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("alert text is missing %q:\n%s", want, text)
		}
	}
}

func TestSOSAlertTextSaysWhenTheLocationIsOnlyThePickup(t *testing.T) {
	payload := tripSOSTriggeredPayload{TripID: "t", AlertID: "alert-1", TriggeredBy: "driver"}
	alert := buildSOSAlert(payload, TripInfo{PickupLatitude: 36.5, PickupLongitude: 44.5}, "", sosTime)

	if !strings.Contains(alert.Text(), "trip pickup, press location unknown") {
		t.Fatalf("the operator must not mistake the pickup for the press location:\n%s", alert.Text())
	}
}

func TestSOSAlertTextForAnUnknownTriggerer(t *testing.T) {
	alert := SOSAlert{TripID: "t", AlertID: "a"}

	if !strings.Contains(alert.Text(), "the user pressed SOS") {
		t.Fatalf("unexpected text: %q", alert.Text())
	}
}

func TestSOSAlertTextStaysWithinTwoSMSSegments(t *testing.T) {
	// A worst-case realistic alert: full UUIDs, a driver name, a location.
	alert := buildSOSAlert(sosSamplePayload(), TripInfo{}, "Abdulrahman Mohammed Al-Barzanji", sosTime)

	if length := len([]rune(alert.Text())); length > 306 {
		t.Fatalf("alert text is %d characters, more than two SMS segments (306)", length)
	}
}
