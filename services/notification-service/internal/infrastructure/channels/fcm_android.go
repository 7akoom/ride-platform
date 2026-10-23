package channels

import (
	"fmt"
	"math"
	"time"
)

// Data keys the sender reads to shape the Android side of a push. They travel to the
// app with the rest of the data, which is harmless.
const (
	// dataAndroidChannel names the app's notification channel for the push (its sound
	// and importance are the app's); such a push also goes out at high priority, so a
	// phone asleep or saving battery shows it at once.
	dataAndroidChannel = "android_channel"
	// dataExpiresAt (RFC 3339) is when the push stops being worth showing: FCM drops it
	// after that instead of delivering it late (a phone that was offline).
	dataExpiresAt = "expires_at"
)

type fcmAndroid struct {
	Priority     string                  `json:"priority,omitempty"`
	TTL          string                  `json:"ttl,omitempty"`
	Notification *fcmAndroidNotification `json:"notification,omitempty"`
}

type fcmAndroidNotification struct {
	ChannelID string `json:"channel_id,omitempty"`
}

// androidOptions is the Android part of a push with this data, or nil for FCM's
// defaults (the app's default channel, normal priority, kept up to four weeks).
func androidOptions(data map[string]string, now time.Time) *fcmAndroid {
	var android fcmAndroid

	if channel := data[dataAndroidChannel]; channel != "" {
		android.Priority = "HIGH"
		android.Notification = &fcmAndroidNotification{ChannelID: channel}
	}

	if expiresAt, err := time.Parse(time.RFC3339Nano, data[dataExpiresAt]); err == nil {
		// At least a second: FCM reads 0 as "deliver now or never", which a push that is
		// only just sent would hit by a hair.
		seconds := math.Max(1, math.Ceil(expiresAt.Sub(now).Seconds()))
		android.TTL = fmt.Sprintf("%.0fs", seconds)
	}

	if android == (fcmAndroid{}) {
		return nil
	}

	return &android
}
