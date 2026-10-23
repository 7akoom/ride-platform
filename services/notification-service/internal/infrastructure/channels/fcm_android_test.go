package channels

import (
	"testing"
	"time"
)

func TestAndroidOptions(t *testing.T) {
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)

	t.Run("plain push keeps FCM defaults", func(t *testing.T) {
		if got := androidOptions(map[string]string{"type": "wallet.transfer_received"}, now); got != nil {
			t.Fatalf("want nil, got %+v", got)
		}
	})

	t.Run("a channel means high priority on that channel", func(t *testing.T) {
		got := androidOptions(map[string]string{dataAndroidChannel: "trip_offers"}, now)
		if got == nil || got.Priority != "HIGH" || got.Notification == nil || got.Notification.ChannelID != "trip_offers" {
			t.Fatalf("got %+v", got)
		}
		if got.TTL != "" {
			t.Fatalf("no expiry, want no ttl, got %q", got.TTL)
		}
	})

	t.Run("an expiry becomes the time to live, rounded up", func(t *testing.T) {
		got := androidOptions(map[string]string{dataExpiresAt: now.Add(9200 * time.Millisecond).Format(time.RFC3339Nano)}, now)
		if got == nil || got.TTL != "10s" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("an expiry already past still lives a second", func(t *testing.T) {
		got := androidOptions(map[string]string{dataExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}, now)
		if got == nil || got.TTL != "1s" {
			t.Fatalf("got %+v", got)
		}
	})
}
