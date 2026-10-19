package notification

import (
	"regexp"
	"testing"
)

func TestPushDataAddsEventKeyAndID(t *testing.T) {
	data := map[string]string{"trip_id": "t-1"}

	got := pushData(" trip.started ", "n-1", data)

	if got["event_key"] != "trip.started" || got["notification_id"] != "n-1" || got["trip_id"] != "t-1" || len(got) != 3 {
		t.Fatalf("pushData = %v", got)
	}

	if _, changed := data["event_key"]; changed {
		t.Fatal("pushData changed the caller's map")
	}
}

func TestPushDataWithNoData(t *testing.T) {
	got := pushData("wallet.money_requested", "n-2", nil)

	if len(got) != 2 || got["event_key"] != "wallet.money_requested" || got["notification_id"] != "n-2" {
		t.Fatalf("pushData = %v", got)
	}
}

func TestNewNotificationIDIsAVersion4UUID(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}

	for range 50 {
		id, err := newNotificationID()
		if err != nil || !shape.MatchString(id) || seen[id] {
			t.Fatalf("newNotificationID = %q, %v", id, err)
		}

		seen[id] = true
	}
}
