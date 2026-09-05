//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

func TestOTPDeliveryTrackingStoreCreateAttempt(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_delivery_tracking_create"

	if _, err := pool.Exec(
		ctx,
		`
			DELETE FROM otp_delivery_attempts
			WHERE challenge_id = $1
		`,
		challengeID,
	); err != nil {
		t.Fatalf(
			"cleanup OTP delivery attempts: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			ctx,
			`
				DELETE FROM otp_delivery_attempts
				WHERE challenge_id = $1
			`,
			challengeID,
		); err != nil {
			t.Errorf(
				"cleanup OTP delivery attempts: %v",
				err,
			)
		}
	})

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := time.Date(
		2026,
		time.August,
		30,
		2,
		30,
		0,
		0,
		time.UTC,
	)

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelSMS,
			Provider:    otp.DeliveryTrackingProviderTelnyx,
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateAttempt() returned an error: %v",
			err,
		)
	}

	if attemptID == "" {
		t.Fatal(
			"CreateAttempt() returned a blank attempt ID",
		)
	}

	var (
		storedID          string
		storedChallengeID string
		storedChannel     string
		storedProvider    string
		storedStatus      string
		storedAttemptedAt time.Time
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				id::text,
				challenge_id,
				channel,
				provider,
				status,
				attempted_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(
		&storedID,
		&storedChallengeID,
		&storedChannel,
		&storedProvider,
		&storedStatus,
		&storedAttemptedAt,
	); err != nil {
		t.Fatalf(
			"query OTP delivery attempt: %v",
			err,
		)
	}

	if storedID != attemptID {
		t.Fatalf(
			"stored attempt ID = %q, want %q",
			storedID,
			attemptID,
		)
	}

	if storedChallengeID != challengeID {
		t.Fatalf(
			"stored challenge ID = %q, want %q",
			storedChallengeID,
			challengeID,
		)
	}

	if storedChannel != string(
		otp.DeliveryTrackingChannelSMS,
	) {
		t.Fatalf(
			"stored channel = %q, want %q",
			storedChannel,
			otp.DeliveryTrackingChannelSMS,
		)
	}

	if storedProvider != string(
		otp.DeliveryTrackingProviderTelnyx,
	) {
		t.Fatalf(
			"stored provider = %q, want %q",
			storedProvider,
			otp.DeliveryTrackingProviderTelnyx,
		)
	}

	if storedStatus != "pending" {
		t.Fatalf(
			"stored status = %q, want %q",
			storedStatus,
			"pending",
		)
	}

	if !storedAttemptedAt.Equal(attemptedAt) {
		t.Fatalf(
			"stored attempted time = %v, want %v",
			storedAttemptedAt,
			attemptedAt,
		)
	}
}
func TestOTPDeliveryTrackingStoreMarkAccepted(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_delivery_tracking_accepted"

	if _, err := pool.Exec(
		ctx,
		`
			DELETE FROM otp_delivery_attempts
			WHERE challenge_id = $1
		`,
		challengeID,
	); err != nil {
		t.Fatalf(
			"cleanup OTP delivery attempts: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			ctx,
			`
				DELETE FROM otp_delivery_attempts
				WHERE challenge_id = $1
			`,
			challengeID,
		); err != nil {
			t.Errorf(
				"cleanup OTP delivery attempts: %v",
				err,
			)
		}
	})

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := time.Date(
		2026,
		time.August,
		30,
		3,
		0,
		0,
		0,
		time.UTC,
	)

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelEmail,
			Provider:    otp.DeliveryTrackingProviderResend,
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateAttempt() returned an error: %v",
			err,
		)
	}

	acceptedAt := attemptedAt.Add(
		250 * time.Millisecond,
	)

	if err := store.MarkAccepted(
		ctx,
		otp.DeliveryAttemptAcceptedInput{
			AttemptID:         attemptID,
			ProviderMessageID: "resend-message-integration-1",
			ProviderStatus:    "accepted",
			AcceptedAt:        acceptedAt,
		},
	); err != nil {
		t.Fatalf(
			"MarkAccepted() returned an error: %v",
			err,
		)
	}

	var (
		storedProviderMessageID string
		storedStatus            string
		storedProviderStatus    string
		storedAcceptedAt        time.Time
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				provider_message_id,
				status,
				last_provider_status,
				accepted_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(
		&storedProviderMessageID,
		&storedStatus,
		&storedProviderStatus,
		&storedAcceptedAt,
	); err != nil {
		t.Fatalf(
			"query accepted OTP delivery attempt: %v",
			err,
		)
	}

	if storedProviderMessageID != "resend-message-integration-1" {
		t.Fatalf(
			"stored provider message ID = %q, want %q",
			storedProviderMessageID,
			"resend-message-integration-1",
		)
	}

	if storedStatus != "accepted" {
		t.Fatalf(
			"stored status = %q, want %q",
			storedStatus,
			"accepted",
		)
	}

	if storedProviderStatus != "accepted" {
		t.Fatalf(
			"stored provider status = %q, want %q",
			storedProviderStatus,
			"accepted",
		)
	}

	if !storedAcceptedAt.Equal(acceptedAt) {
		t.Fatalf(
			"stored accepted time = %v, want %v",
			storedAcceptedAt,
			acceptedAt,
		)
	}

	err = store.MarkAccepted(
		ctx,
		otp.DeliveryAttemptAcceptedInput{
			AttemptID:         attemptID,
			ProviderMessageID: "resend-message-integration-2",
			ProviderStatus:    "accepted",
			AcceptedAt: acceptedAt.Add(
				time.Second,
			),
		},
	)
	if err == nil {
		t.Fatal(
			"second MarkAccepted() returned nil error",
		)
	}
}
func TestOTPDeliveryTrackingStoreMarkFailed(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_delivery_tracking_failed"

	if _, err := pool.Exec(
		ctx,
		`
			DELETE FROM otp_delivery_attempts
			WHERE challenge_id = $1
		`,
		challengeID,
	); err != nil {
		t.Fatalf(
			"cleanup OTP delivery attempts: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			ctx,
			`
				DELETE FROM otp_delivery_attempts
				WHERE challenge_id = $1
			`,
			challengeID,
		); err != nil {
			t.Errorf(
				"cleanup OTP delivery attempts: %v",
				err,
			)
		}
	})

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := time.Date(
		2026,
		time.August,
		30,
		3,
		30,
		0,
		0,
		time.UTC,
	)

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelSMS,
			Provider:    otp.DeliveryTrackingProviderTelnyx,
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateAttempt() returned an error: %v",
			err,
		)
	}

	failedAt := attemptedAt.Add(
		500 * time.Millisecond,
	)

	if err := store.MarkFailed(
		ctx,
		otp.DeliveryAttemptFailedInput{
			AttemptID:      attemptID,
			ProviderStatus: "failed",
			FailureCode:    "provider_rejected",
			FailedAt:       failedAt,
		},
	); err != nil {
		t.Fatalf(
			"MarkFailed() returned an error: %v",
			err,
		)
	}

	var (
		storedStatus         string
		storedProviderStatus string
		storedFailureCode    string
		storedFailedAt       time.Time
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				status,
				last_provider_status,
				failure_code,
				failed_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(
		&storedStatus,
		&storedProviderStatus,
		&storedFailureCode,
		&storedFailedAt,
	); err != nil {
		t.Fatalf(
			"query failed OTP delivery attempt: %v",
			err,
		)
	}

	if storedStatus != "failed" {
		t.Fatalf(
			"stored status = %q, want %q",
			storedStatus,
			"failed",
		)
	}

	if storedProviderStatus != "failed" {
		t.Fatalf(
			"stored provider status = %q, want %q",
			storedProviderStatus,
			"failed",
		)
	}

	if storedFailureCode != "provider_rejected" {
		t.Fatalf(
			"stored failure code = %q, want %q",
			storedFailureCode,
			"provider_rejected",
		)
	}

	if !storedFailedAt.Equal(failedAt) {
		t.Fatalf(
			"stored failed time = %v, want %v",
			storedFailedAt,
			failedAt,
		)
	}
}

func TestOTPDeliveryTrackingStoreMarkUnknown(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_delivery_tracking_unknown"

	if _, err := pool.Exec(
		ctx,
		`
			DELETE FROM otp_delivery_attempts
			WHERE challenge_id = $1
		`,
		challengeID,
	); err != nil {
		t.Fatalf(
			"cleanup OTP delivery attempts: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			ctx,
			`
				DELETE FROM otp_delivery_attempts
				WHERE challenge_id = $1
			`,
			challengeID,
		); err != nil {
			t.Errorf(
				"cleanup OTP delivery attempts: %v",
				err,
			)
		}
	})

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := time.Date(
		2026,
		time.August,
		30,
		4,
		0,
		0,
		0,
		time.UTC,
	)

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelWhatsApp,
			Provider:    otp.DeliveryTrackingProviderMeta,
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateAttempt() returned an error: %v",
			err,
		)
	}

	if err := store.MarkUnknown(
		ctx,
		otp.DeliveryAttemptUnknownInput{
			AttemptID:      attemptID,
			ProviderStatus: "network_timeout",
		},
	); err != nil {
		t.Fatalf(
			"MarkUnknown() returned an error: %v",
			err,
		)
	}

	var (
		storedStatus         string
		storedProviderStatus string
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				status,
				last_provider_status
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(
		&storedStatus,
		&storedProviderStatus,
	); err != nil {
		t.Fatalf(
			"query unknown OTP delivery attempt: %v",
			err,
		)
	}

	if storedStatus != "unknown" {
		t.Fatalf(
			"stored status = %q, want %q",
			storedStatus,
			"unknown",
		)
	}

	if storedProviderStatus != "network_timeout" {
		t.Fatalf(
			"stored provider status = %q, want %q",
			storedProviderStatus,
			"network_timeout",
		)
	}
}
