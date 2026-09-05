package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OTPDeliveryTrackingStore struct {
	pool *pgxpool.Pool
}

var _ otp.DeliveryTrackingStore = (*OTPDeliveryTrackingStore)(nil)

var _ otp.DeliveryReceiptStore = (*OTPDeliveryTrackingStore)(nil)

func NewOTPDeliveryTrackingStore(
	pool *pgxpool.Pool,
) *OTPDeliveryTrackingStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &OTPDeliveryTrackingStore{
		pool: pool,
	}
}

func (s *OTPDeliveryTrackingStore) CreateAttempt(
	ctx context.Context,
	input otp.DeliveryAttemptCreateInput,
) (string, error) {
	challengeID := strings.TrimSpace(input.ChallengeID)
	if challengeID == "" {
		return "", errors.New(
			"OTP delivery challenge ID cannot be blank",
		)
	}

	if len(challengeID) > 64 {
		return "", errors.New(
			"OTP delivery challenge ID is too long",
		)
	}

	switch input.Channel {
	case otp.DeliveryTrackingChannelSMS,
		otp.DeliveryTrackingChannelWhatsApp,
		otp.DeliveryTrackingChannelEmail:
	default:
		return "", errors.New(
			"OTP delivery channel is invalid",
		)
	}

	switch input.Provider {
	case otp.DeliveryTrackingProviderTelnyx,
		otp.DeliveryTrackingProviderMeta,
		otp.DeliveryTrackingProviderBulkSMSIraq,
		otp.DeliveryTrackingProviderResend,
		otp.DeliveryTrackingProviderDevelopment:
	default:
		return "", errors.New(
			"OTP delivery provider is invalid",
		)
	}

	attemptedAt := input.AttemptedAt
	if attemptedAt.IsZero() {
		return "", errors.New(
			"OTP delivery attempted time cannot be zero",
		)
	}

	attemptedAt = attemptedAt.UTC()

	const query = `
		INSERT INTO otp_delivery_attempts (
			challenge_id,
			channel,
			provider,
			attempted_at
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text
	`

	var attemptID string

	if err := s.pool.QueryRow(
		ctx,
		query,
		challengeID,
		string(input.Channel),
		string(input.Provider),
		attemptedAt,
	).Scan(&attemptID); err != nil {
		return "", fmt.Errorf(
			"insert OTP delivery attempt: %w",
			err,
		)
	}

	if strings.TrimSpace(attemptID) == "" {
		return "", errors.New(
			"insert OTP delivery attempt returned blank ID",
		)
	}

	return attemptID, nil
}
func (s *OTPDeliveryTrackingStore) MarkAccepted(
	ctx context.Context,
	input otp.DeliveryAttemptAcceptedInput,
) error {
	attemptID := strings.TrimSpace(input.AttemptID)
	if attemptID == "" {
		return errors.New(
			"OTP delivery attempt ID cannot be blank",
		)
	}

	providerMessageID := strings.TrimSpace(
		input.ProviderMessageID,
	)
	if providerMessageID == "" {
		return errors.New(
			"OTP delivery provider message ID cannot be blank",
		)
	}

	if len(providerMessageID) > 255 {
		return errors.New(
			"OTP delivery provider message ID is too long",
		)
	}

	providerStatus := strings.TrimSpace(
		input.ProviderStatus,
	)

	if len(providerStatus) > 100 {
		return errors.New(
			"OTP delivery provider status is too long",
		)
	}

	var providerStatusValue any
	if providerStatus != "" {
		providerStatusValue = providerStatus
	}

	acceptedAt := input.AcceptedAt
	if acceptedAt.IsZero() {
		return errors.New(
			"OTP delivery accepted time cannot be zero",
		)
	}

	acceptedAt = acceptedAt.UTC()

	const query = `
		UPDATE otp_delivery_attempts
		SET
			provider_message_id = $2,
			status = 'accepted',
			last_provider_status = $3,
			accepted_at = $4,
			updated_at = statement_timestamp()
		WHERE id = $1::uuid
		AND status = 'pending'
	`

	result, err := s.pool.Exec(
		ctx,
		query,
		attemptID,
		providerMessageID,
		providerStatusValue,
		acceptedAt,
	)
	if err != nil {
		return fmt.Errorf(
			"mark OTP delivery attempt accepted: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return errors.New(
			"OTP delivery attempt was not pending",
		)
	}

	return nil
}
func (s *OTPDeliveryTrackingStore) MarkFailed(
	ctx context.Context,
	input otp.DeliveryAttemptFailedInput,
) error {
	attemptID := strings.TrimSpace(input.AttemptID)
	if attemptID == "" {
		return errors.New(
			"OTP delivery attempt ID cannot be blank",
		)
	}

	providerStatus := strings.TrimSpace(
		input.ProviderStatus,
	)
	if len(providerStatus) > 100 {
		return errors.New(
			"OTP delivery provider status is too long",
		)
	}

	failureCode := strings.TrimSpace(
		input.FailureCode,
	)
	if len(failureCode) > 100 {
		return errors.New(
			"OTP delivery failure code is too long",
		)
	}

	failedAt := input.FailedAt
	if failedAt.IsZero() {
		return errors.New(
			"OTP delivery failed time cannot be zero",
		)
	}

	failedAt = failedAt.UTC()

	var providerStatusValue any
	if providerStatus != "" {
		providerStatusValue = providerStatus
	}

	var failureCodeValue any
	if failureCode != "" {
		failureCodeValue = failureCode
	}

	const query = `
		UPDATE otp_delivery_attempts
		SET
			status = 'failed',
			last_provider_status = $2,
			failure_code = $3,
			failed_at = $4,
			updated_at = statement_timestamp()
		WHERE id = $1::uuid
		AND status = 'pending'
	`

	result, err := s.pool.Exec(
		ctx,
		query,
		attemptID,
		providerStatusValue,
		failureCodeValue,
		failedAt,
	)
	if err != nil {
		return fmt.Errorf(
			"mark OTP delivery attempt failed: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return errors.New(
			"OTP delivery attempt was not pending",
		)
	}

	return nil
}

func (s *OTPDeliveryTrackingStore) MarkUnknown(
	ctx context.Context,
	input otp.DeliveryAttemptUnknownInput,
) error {
	attemptID := strings.TrimSpace(input.AttemptID)
	if attemptID == "" {
		return errors.New(
			"OTP delivery attempt ID cannot be blank",
		)
	}

	providerStatus := strings.TrimSpace(
		input.ProviderStatus,
	)
	if len(providerStatus) > 100 {
		return errors.New(
			"OTP delivery provider status is too long",
		)
	}

	var providerStatusValue any
	if providerStatus != "" {
		providerStatusValue = providerStatus
	}

	const query = `
		UPDATE otp_delivery_attempts
		SET
			status = 'unknown',
			last_provider_status = $2,
			updated_at = statement_timestamp()
		WHERE id = $1::uuid
		AND status = 'pending'
	`

	result, err := s.pool.Exec(
		ctx,
		query,
		attemptID,
		providerStatusValue,
	)
	if err != nil {
		return fmt.Errorf(
			"mark OTP delivery attempt unknown: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return errors.New(
			"OTP delivery attempt was not pending",
		)
	}

	return nil
}
func (s *OTPDeliveryTrackingStore) ApplyReceipt(
	ctx context.Context,
	input otp.DeliveryReceiptInput,
) error {
	switch input.Provider {
	case otp.DeliveryTrackingProviderTelnyx,
		otp.DeliveryTrackingProviderMeta,
		otp.DeliveryTrackingProviderBulkSMSIraq,
		otp.DeliveryTrackingProviderResend,
		otp.DeliveryTrackingProviderDevelopment:
	default:
		return errors.New(
			"OTP delivery receipt provider is invalid",
		)
	}

	providerMessageID := strings.TrimSpace(
		input.ProviderMessageID,
	)
	if providerMessageID == "" {
		return errors.New(
			"OTP delivery receipt provider message ID cannot be blank",
		)
	}

	if len(providerMessageID) > 255 {
		return errors.New(
			"OTP delivery receipt provider message ID is too long",
		)
	}

	switch input.Status {
	case otp.DeliveryReceiptStatusSent,
		otp.DeliveryReceiptStatusDelivered,
		otp.DeliveryReceiptStatusFailed:
	default:
		return errors.New(
			"OTP delivery receipt status is invalid",
		)
	}

	providerStatus := strings.TrimSpace(
		input.ProviderStatus,
	)
	if len(providerStatus) > 100 {
		return errors.New(
			"OTP delivery receipt provider status is too long",
		)
	}

	failureCode := strings.TrimSpace(
		input.FailureCode,
	)
	if len(failureCode) > 100 {
		return errors.New(
			"OTP delivery receipt failure code is too long",
		)
	}

	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		return errors.New(
			"OTP delivery receipt occurred time cannot be zero",
		)
	}

	occurredAt = occurredAt.UTC()

	var providerStatusValue any
	if providerStatus != "" {
		providerStatusValue = providerStatus
	}

	var failureCodeValue any
	if failureCode != "" {
		failureCodeValue = failureCode
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin OTP delivery receipt transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const lockQuery = `
		SELECT
			id::text,
			status,
			CASE status
				WHEN 'accepted' THEN
					COALESCE(
						accepted_at,
						attempted_at
					)

				WHEN 'sent' THEN
					COALESCE(
						sent_at,
						accepted_at,
						attempted_at
					)

				WHEN 'delivered' THEN
					COALESCE(
						delivered_at,
						sent_at,
						accepted_at,
						attempted_at
					)

				WHEN 'failed' THEN
					COALESCE(
						failed_at,
						sent_at,
						accepted_at,
						attempted_at
					)

				ELSE attempted_at
			END
		FROM otp_delivery_attempts
		WHERE provider = $1
		AND provider_message_id = $2
		FOR UPDATE
	`

	var attemptID string
	var currentStatus string

	stateOccurredAt := occurredAt

	err = tx.QueryRow(
		ctx,
		lockQuery,
		string(input.Provider),
		providerMessageID,
	).Scan(
		&attemptID,
		&currentStatus,
		&stateOccurredAt,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return errors.New(
				"OTP delivery attempt was not found for provider message ID",
			)
		}

		return fmt.Errorf(
			"lock OTP delivery attempt for receipt: %w",
			err,
		)
	}

	currentStatus = strings.TrimSpace(
		currentStatus,
	)

	switch currentStatus {
	case "delivered",
		"failed":
		return nil
	}

	if occurredAt.Before(
		stateOccurredAt.UTC(),
	) {
		return nil
	}

	var query string
	var args []any

	switch input.Status {
	case otp.DeliveryReceiptStatusSent:
		switch currentStatus {
		case "sent":
			return nil

		case "pending",
			"accepted",
			"unknown":

		default:
			return fmt.Errorf(
				"OTP delivery receipt cannot transition status from %q to sent",
				currentStatus,
			)
		}

		query = `
			UPDATE otp_delivery_attempts
			SET
				status = 'sent',
				last_provider_status =
					COALESCE(
						$2,
						last_provider_status
					),
				failure_code = NULL,
				sent_at = $3,
				updated_at = statement_timestamp()
			WHERE id = $1::uuid
		`

		args = []any{
			attemptID,
			providerStatusValue,
			occurredAt,
		}

	case otp.DeliveryReceiptStatusDelivered:
		switch currentStatus {
		case "pending",
			"accepted",
			"unknown",
			"sent":

		default:
			return fmt.Errorf(
				"OTP delivery receipt cannot transition status from %q to delivered",
				currentStatus,
			)
		}

		query = `
			UPDATE otp_delivery_attempts
			SET
				status = 'delivered',
				last_provider_status =
					COALESCE(
						$2,
						last_provider_status
					),
				failure_code = NULL,
				delivered_at = $3,
				updated_at = statement_timestamp()
			WHERE id = $1::uuid
		`

		args = []any{
			attemptID,
			providerStatusValue,
			occurredAt,
		}

	case otp.DeliveryReceiptStatusFailed:
		switch currentStatus {
		case "pending",
			"accepted",
			"unknown",
			"sent":

		default:
			return fmt.Errorf(
				"OTP delivery receipt cannot transition status from %q to failed",
				currentStatus,
			)
		}

		query = `
			UPDATE otp_delivery_attempts
			SET
				status = 'failed',
				last_provider_status =
					COALESCE(
						$2,
						last_provider_status
					),
				failure_code =
					COALESCE(
						$3,
						failure_code
					),
				failed_at = $4,
				updated_at = statement_timestamp()
			WHERE id = $1::uuid
		`

		args = []any{
			attemptID,
			providerStatusValue,
			failureCodeValue,
			occurredAt,
		}
	}

	result, err := tx.Exec(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return fmt.Errorf(
			"apply OTP delivery receipt: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return errors.New(
			"OTP delivery receipt did not update an attempt",
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit OTP delivery receipt transaction: %w",
			err,
		)
	}

	return nil
}
