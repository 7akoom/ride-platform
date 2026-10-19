package notification

import "testing"

func TestPushDataAddsEventKey(t *testing.T) {
	data := map[string]string{"trip_id": "t-1"}

	got := pushData(" trip.started ", data)

	if got["event_key"] != "trip.started" || got["trip_id"] != "t-1" || len(got) != 2 {
		t.Fatalf("pushData = %v", got)
	}

	if _, changed := data["event_key"]; changed {
		t.Fatal("pushData changed the caller's map")
	}
}

func TestPushDataWithNoData(t *testing.T) {
	got := pushData("wallet.money_requested", nil)

	if len(got) != 1 || got["event_key"] != "wallet.money_requested" {
		t.Fatalf("pushData = %v", got)
	}
}
