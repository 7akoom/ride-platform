package otp

import (
	"context"
	"time"
)

type DeliveryTrackingChannel string

const (
	DeliveryTrackingChannelSMS      DeliveryTrackingChannel = "sms"
	DeliveryTrackingChannelWhatsApp DeliveryTrackingChannel = "whatsapp"
	DeliveryTrackingChannelEmail    DeliveryTrackingChannel = "email"
)

type DeliveryTrackingProvider string

const (
	DeliveryTrackingProviderTelnyx      DeliveryTrackingProvider = "telnyx"
	DeliveryTrackingProviderMeta        DeliveryTrackingProvider = "meta"
	DeliveryTrackingProviderBulkSMSIraq DeliveryTrackingProvider = "bulksmsiraq"
	DeliveryTrackingProviderResend      DeliveryTrackingProvider = "resend"
	DeliveryTrackingProviderDevelopment DeliveryTrackingProvider = "development"
)

type DeliveryAttemptCreateInput struct {
	ChallengeID string
	Channel     DeliveryTrackingChannel
	Provider    DeliveryTrackingProvider
	AttemptedAt time.Time
}

type DeliveryAttemptAcceptedInput struct {
	AttemptID         string
	ProviderMessageID string
	ProviderStatus    string
	AcceptedAt        time.Time
}

type DeliveryAttemptFailedInput struct {
	AttemptID      string
	ProviderStatus string
	FailureCode    string
	FailedAt       time.Time
}

type DeliveryAttemptUnknownInput struct {
	AttemptID      string
	ProviderStatus string
}

type DeliveryTrackingStore interface {
	CreateAttempt(
		ctx context.Context,
		input DeliveryAttemptCreateInput,
	) (string, error)

	MarkAccepted(
		ctx context.Context,
		input DeliveryAttemptAcceptedInput,
	) error

	MarkFailed(
		ctx context.Context,
		input DeliveryAttemptFailedInput,
	) error

	MarkUnknown(
		ctx context.Context,
		input DeliveryAttemptUnknownInput,
	) error
}

type DeliveryReceiptStatus string

const (
	DeliveryReceiptStatusSent      DeliveryReceiptStatus = "sent"
	DeliveryReceiptStatusDelivered DeliveryReceiptStatus = "delivered"
	DeliveryReceiptStatusFailed    DeliveryReceiptStatus = "failed"
)

type DeliveryReceiptInput struct {
	Provider          DeliveryTrackingProvider
	ProviderMessageID string
	Status            DeliveryReceiptStatus
	ProviderStatus    string
	FailureCode       string
	OccurredAt        time.Time
}

type DeliveryReceiptStore interface {
	ApplyReceipt(
		ctx context.Context,
		input DeliveryReceiptInput,
	) error
}

type NoopDeliveryTrackingStore struct{}

func (NoopDeliveryTrackingStore) CreateAttempt(
	context.Context,
	DeliveryAttemptCreateInput,
) (string, error) {
	return "", nil
}

func (NoopDeliveryTrackingStore) MarkAccepted(
	context.Context,
	DeliveryAttemptAcceptedInput,
) error {
	return nil
}

func (NoopDeliveryTrackingStore) MarkFailed(
	context.Context,
	DeliveryAttemptFailedInput,
) error {
	return nil
}

func (NoopDeliveryTrackingStore) MarkUnknown(
	context.Context,
	DeliveryAttemptUnknownInput,
) error {
	return nil
}

type NoopDeliveryReceiptStore struct{}

func (NoopDeliveryReceiptStore) ApplyReceipt(
	context.Context,
	DeliveryReceiptInput,
) error {
	return nil
}
