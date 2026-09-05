package otp

import (
	"testing"
	"time"
)

func TestCircuitBreakerProviderHealthTrackerStartsHealthy(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		3,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	if !tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at,
	) {
		t.Fatal(
			"CanAttempt() = false for healthy provider, want true",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerOpensAfterThreshold(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		3,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	for i := 0; i < 2; i++ {
		tracker.RecordFailure(
			DeliveryTrackingChannelSMS,
			DeliveryTrackingProviderBulkSMSIraq,
			at.Add(
				time.Duration(i)*time.Second,
			),
		)

		if !tracker.CanAttempt(
			DeliveryTrackingChannelSMS,
			DeliveryTrackingProviderBulkSMSIraq,
			at.Add(
				time.Duration(i+1)*time.Second,
			),
		) {
			t.Fatalf(
				"CanAttempt() = false after failure %d, want true",
				i+1,
			)
		}
	}

	thirdFailureAt := at.Add(
		2 * time.Second,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		thirdFailureAt,
	)

	if tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		thirdFailureAt.Add(
			30*time.Second,
		),
	) {
		t.Fatal(
			"CanAttempt() = true while circuit is open, want false",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerAllowsAttemptAfterCooldown(
	t *testing.T,
) {
	cooldown := time.Minute

	tracker, err := NewCircuitBreakerProviderHealthTracker(
		2,
		cooldown,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at,
	)

	secondFailureAt := at.Add(
		time.Second,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		secondFailureAt,
	)

	if tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		secondFailureAt.Add(
			cooldown-time.Millisecond,
		),
	) {
		t.Fatal(
			"CanAttempt() = true before cooldown expires, want false",
		)
	}

	if !tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		secondFailureAt.Add(
			cooldown,
		),
	) {
		t.Fatal(
			"CanAttempt() = false after cooldown expires, want true",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerSuccessResetsFailures(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		3,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(time.Second),
	)

	tracker.RecordSuccess(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(2*time.Second),
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(3*time.Second),
	)

	if !tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(4*time.Second),
	) {
		t.Fatal(
			"CanAttempt() = false after success reset and two failures, want true",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerSeparatesProviders(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at,
	)

	if tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(time.Second),
	) {
		t.Fatal(
			"BulkSMSIraq circuit remained available after threshold",
		)
	}

	if !tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderTelnyx,
		at.Add(time.Second),
	) {
		t.Fatal(
			"Telnyx circuit was affected by BulkSMSIraq failure",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerSeparatesChannels(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at,
	)

	if tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(time.Second),
	) {
		t.Fatal(
			"SMS circuit remained available after threshold",
		)
	}

	if !tracker.CanAttempt(
		DeliveryTrackingChannelWhatsApp,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(time.Second),
	) {
		t.Fatal(
			"WhatsApp circuit was affected by SMS failure",
		)
	}
}

func TestCircuitBreakerProviderHealthTrackerNormalizesKey(
	t *testing.T,
) {
	tracker, err := NewCircuitBreakerProviderHealthTracker(
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewCircuitBreakerProviderHealthTracker() returned an error: %v",
			err,
		)
	}

	at := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	tracker.RecordFailure(
		DeliveryTrackingChannel(" SMS "),
		DeliveryTrackingProvider(" BULKSMSIRAQ "),
		at,
	)

	if tracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProviderBulkSMSIraq,
		at.Add(time.Second),
	) {
		t.Fatal(
			"normalized provider health key did not share circuit state",
		)
	}
}

func TestNewCircuitBreakerProviderHealthTrackerRejectsInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name             string
		failureThreshold int
		cooldown         time.Duration
	}{
		{
			name:             "zero threshold",
			failureThreshold: 0,
			cooldown:         time.Minute,
		},
		{
			name:             "negative threshold",
			failureThreshold: -1,
			cooldown:         time.Minute,
		},
		{
			name:             "zero cooldown",
			failureThreshold: 3,
			cooldown:         0,
		},
		{
			name:             "negative cooldown",
			failureThreshold: 3,
			cooldown:         -time.Second,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				tracker, err :=
					NewCircuitBreakerProviderHealthTracker(
						testCase.failureThreshold,
						testCase.cooldown,
					)

				if err == nil {
					t.Fatal(
						"NewCircuitBreakerProviderHealthTracker() accepted invalid configuration",
					)
				}

				if tracker != nil {
					t.Fatal(
						"NewCircuitBreakerProviderHealthTracker() returned tracker for invalid configuration",
					)
				}
			},
		)
	}
}
