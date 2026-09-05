//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOTPDeliveryTrackingStoreApplyReceiptAcceptedToSentToDelivered(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_sent_delivered",
			"bulksmsiraq-receipt-sent-delivered",
		)

	sentAt := acceptedAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-sent-delivered",
			Status:            otp.DeliveryReceiptStatusSent,
			ProviderStatus:    "sent",
			OccurredAt:        sentAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(sent) returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"sent",
	)

	var storedSentAt time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT sent_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(&storedSentAt); err != nil {
		t.Fatalf(
			"query sent time: %v",
			err,
		)
	}

	if !storedSentAt.Equal(sentAt) {
		t.Fatalf(
			"stored sent time = %v, want %v",
			storedSentAt,
			sentAt,
		)
	}

	deliveredAt := sentAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-sent-delivered",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt:        deliveredAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(delivered) returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"delivered",
	)

	var storedDeliveredAt time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT delivered_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(&storedDeliveredAt); err != nil {
		t.Fatalf(
			"query delivered time: %v",
			err,
		)
	}

	if !storedDeliveredAt.Equal(deliveredAt) {
		t.Fatalf(
			"stored delivered time = %v, want %v",
			storedDeliveredAt,
			deliveredAt,
		)
	}
}

func TestOTPDeliveryTrackingStoreApplyReceiptAcceptedToDelivered(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_direct_delivered",
			"bulksmsiraq-receipt-direct-delivered",
		)

	deliveredAt := acceptedAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-direct-delivered",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt:        deliveredAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt() returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"delivered",
	)
}

func TestOTPDeliveryTrackingStoreApplyReceiptUnknownToDelivered(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	const (
		challengeID       = "otp_ch_receipt_unknown_delivered"
		providerMessageID = "bulksmsiraq-receipt-unknown-delivered"
	)

	cleanupDeliveryReceiptAttempt(
		t,
		ctx,
		pool,
		challengeID,
	)

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := deliveryReceiptTestAttemptedAt()

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelWhatsApp,
			Provider:    otp.DeliveryTrackingProviderBulkSMSIraq,
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateAttempt() returned an error: %v",
			err,
		)
	}

	if _, err := pool.Exec(
		ctx,
		`
			UPDATE otp_delivery_attempts
			SET
				provider_message_id = $2,
				status = 'unknown',
				last_provider_status = 'unknown',
				updated_at = statement_timestamp()
			WHERE id = $1::uuid
		`,
		attemptID,
		providerMessageID,
	); err != nil {
		t.Fatalf(
			"prepare unknown OTP delivery attempt: %v",
			err,
		)
	}

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: providerMessageID,
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt:        attemptedAt.Add(2 * time.Second),
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt() returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"delivered",
	)
}

func TestOTPDeliveryTrackingStoreApplyReceiptSentToFailed(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_sent_failed",
			"bulksmsiraq-receipt-sent-failed",
		)

	sentAt := acceptedAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-sent-failed",
			Status:            otp.DeliveryReceiptStatusSent,
			ProviderStatus:    "sent",
			OccurredAt:        sentAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(sent) returned an error: %v",
			err,
		)
	}

	failedAt := sentAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-sent-failed",
			Status:            otp.DeliveryReceiptStatusFailed,
			ProviderStatus:    "failed",
			FailureCode:       "provider_rejected",
			OccurredAt:        failedAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(failed) returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"failed",
	)

	var (
		storedFailureCode string
		storedFailedAt    time.Time
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				COALESCE(failure_code, ''),
				failed_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(
		&storedFailureCode,
		&storedFailedAt,
	); err != nil {
		t.Fatalf(
			"query failed OTP delivery attempt: %v",
			err,
		)
	}

	if storedFailureCode != "provider_rejected" {
		t.Fatalf(
			"failure code = %q, want %q",
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

func TestOTPDeliveryTrackingStoreApplyReceiptDuplicateDeliveredIsIdempotent(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_duplicate_delivered",
			"bulksmsiraq-receipt-duplicate-delivered",
		)

	deliveredAt := acceptedAt.Add(time.Second)

	receipt := otp.DeliveryReceiptInput{
		Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
		ProviderMessageID: "bulksmsiraq-receipt-duplicate-delivered",
		Status:            otp.DeliveryReceiptStatusDelivered,
		ProviderStatus:    "delivered",
		OccurredAt:        deliveredAt,
	}

	if err := store.ApplyReceipt(
		ctx,
		receipt,
	); err != nil {
		t.Fatalf(
			"first ApplyReceipt() returned an error: %v",
			err,
		)
	}

	if err := store.ApplyReceipt(
		ctx,
		receipt,
	); err != nil {
		t.Fatalf(
			"duplicate ApplyReceipt() returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"delivered",
	)

	var storedDeliveredAt time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT delivered_at
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(&storedDeliveredAt); err != nil {
		t.Fatalf(
			"query delivered time: %v",
			err,
		)
	}

	if !storedDeliveredAt.Equal(deliveredAt) {
		t.Fatalf(
			"duplicate receipt changed delivered time to %v, want %v",
			storedDeliveredAt,
			deliveredAt,
		)
	}
}

func TestOTPDeliveryTrackingStoreApplyReceiptDoesNotDowngradeDeliveredToSent(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_delivered_sent",
			"bulksmsiraq-receipt-delivered-sent",
		)

	deliveredAt := acceptedAt.Add(2 * time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-delivered-sent",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt:        deliveredAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(delivered) returned an error: %v",
			err,
		)
	}

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-delivered-sent",
			Status:            otp.DeliveryReceiptStatusSent,
			ProviderStatus:    "sent",
			OccurredAt:        deliveredAt.Add(time.Second),
		},
	); err != nil {
		t.Fatalf(
			"late ApplyReceipt(sent) returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"delivered",
	)
}

func TestOTPDeliveryTrackingStoreApplyReceiptDoesNotChangeFailedToDelivered(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_failed_delivered",
			"bulksmsiraq-receipt-failed-delivered",
		)

	failedAt := acceptedAt.Add(time.Second)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-failed-delivered",
			Status:            otp.DeliveryReceiptStatusFailed,
			ProviderStatus:    "failed",
			FailureCode:       "rejected",
			OccurredAt:        failedAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(failed) returned an error: %v",
			err,
		)
	}

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-failed-delivered",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt:        failedAt.Add(time.Second),
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt(delivered) after failed returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"failed",
	)
}

func TestOTPDeliveryTrackingStoreApplyReceiptIgnoresOlderReceipt(
	t *testing.T,
) {
	ctx, pool, store, attemptID, acceptedAt :=
		createAcceptedReceiptAttempt(
			t,
			"otp_ch_receipt_older",
			"bulksmsiraq-receipt-older",
		)

	olderSentAt := acceptedAt.Add(
		-time.Millisecond,
	)

	if err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-older",
			Status:            otp.DeliveryReceiptStatusSent,
			ProviderStatus:    "sent",
			OccurredAt:        olderSentAt,
		},
	); err != nil {
		t.Fatalf(
			"ApplyReceipt() returned an error: %v",
			err,
		)
	}

	assertDeliveryAttemptStatus(
		t,
		ctx,
		pool,
		attemptID,
		"accepted",
	)
}

func TestOTPDeliveryTrackingStoreApplyReceiptRejectsUnknownProviderMessageID(
	t *testing.T,
) {
	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	store := NewOTPDeliveryTrackingStore(pool)

	err := store.ApplyReceipt(
		ctx,
		otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "bulksmsiraq-receipt-does-not-exist",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt: deliveryReceiptTestAttemptedAt().Add(
				time.Minute,
			),
		},
	)

	if err == nil {
		t.Fatal(
			"ApplyReceipt() returned nil error for unknown provider message ID",
		)
	}
}

func createAcceptedReceiptAttempt(
	t *testing.T,
	challengeID string,
	providerMessageID string,
) (
	context.Context,
	*pgxpool.Pool,
	*OTPDeliveryTrackingStore,
	string,
	time.Time,
) {
	t.Helper()

	ctx, pool, _ := newChallengeRepositoryIntegrationTest(t)

	cleanupDeliveryReceiptAttempt(
		t,
		ctx,
		pool,
		challengeID,
	)

	store := NewOTPDeliveryTrackingStore(pool)

	attemptedAt := deliveryReceiptTestAttemptedAt()

	attemptID, err := store.CreateAttempt(
		ctx,
		otp.DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     otp.DeliveryTrackingChannelSMS,
			Provider:    otp.DeliveryTrackingProviderBulkSMSIraq,
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
		500 * time.Millisecond,
	)

	if err := store.MarkAccepted(
		ctx,
		otp.DeliveryAttemptAcceptedInput{
			AttemptID:         attemptID,
			ProviderMessageID: providerMessageID,
			ProviderStatus:    "accepted",
			AcceptedAt:        acceptedAt,
		},
	); err != nil {
		t.Fatalf(
			"MarkAccepted() returned an error: %v",
			err,
		)
	}

	return ctx,
		pool,
		store,
		attemptID,
		acceptedAt
}

func cleanupDeliveryReceiptAttempt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	challengeID string,
) {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		`
			DELETE FROM otp_delivery_attempts
			WHERE challenge_id = $1
		`,
		challengeID,
	); err != nil {
		t.Fatalf(
			"cleanup OTP delivery attempts before test: %v",
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
				"cleanup OTP delivery attempts after test: %v",
				err,
			)
		}
	})
}

func assertDeliveryAttemptStatus(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	attemptID string,
	expectedStatus string,
) {
	t.Helper()

	var storedStatus string

	if err := pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM otp_delivery_attempts
			WHERE id = $1::uuid
		`,
		attemptID,
	).Scan(&storedStatus); err != nil {
		t.Fatalf(
			"query OTP delivery attempt status: %v",
			err,
		)
	}

	if storedStatus != expectedStatus {
		t.Fatalf(
			"stored status = %q, want %q",
			storedStatus,
			expectedStatus,
		)
	}
}

func deliveryReceiptTestAttemptedAt() time.Time {
	return time.Date(
		2026,
		time.August,
		31,
		16,
		0,
		0,
		0,
		time.UTC,
	)
}
