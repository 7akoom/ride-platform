package otp

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	telnyxWebhookTimestampHeader = "telnyx-timestamp"
	telnyxWebhookSignatureHeader = "telnyx-signature-ed25519"

	telnyxWebhookTimestampTolerance = 5 * time.Minute
	telnyxWebhookMaxBodyBytes       = 128 * 1024
)

type TelnyxDeliveryReceiptDecoder struct {
	publicKey ed25519.PublicKey
	now       func() time.Time
}

type telnyxDeliveryWebhook struct {
	Data struct {
		EventType  string    `json:"event_type"`
		OccurredAt time.Time `json:"occurred_at"`
		Payload    struct {
			ID string `json:"id"`

			To []struct {
				Status string `json:"status"`
			} `json:"to"`

			Errors []struct {
				Code string `json:"code"`
			} `json:"errors"`
		} `json:"payload"`
	} `json:"data"`
}

func NewTelnyxDeliveryReceiptDecoder(
	publicKey string,
) (*TelnyxDeliveryReceiptDecoder, error) {
	publicKey = strings.TrimSpace(
		publicKey,
	)

	if publicKey == "" {
		return nil, errors.New(
			"Telnyx webhook public key is required",
		)
	}

	publicKeyBytes, err := base64.StdEncoding.DecodeString(
		publicKey,
	)
	if err != nil {
		return nil, errors.New(
			"Telnyx webhook public key must be valid base64",
		)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf(
			"Telnyx webhook public key must be %d bytes",
			ed25519.PublicKeySize,
		)
	}

	return &TelnyxDeliveryReceiptDecoder{
		publicKey: ed25519.PublicKey(
			publicKeyBytes,
		),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}, nil
}

func (d *TelnyxDeliveryReceiptDecoder) Decode(
	_ context.Context,
	request *http.Request,
) (DeliveryReceiptInput, error) {
	if d == nil ||
		len(d.publicKey) != ed25519.PublicKeySize ||
		d.now == nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	if request == nil ||
		request.Body == nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	timestamp := strings.TrimSpace(
		request.Header.Get(
			telnyxWebhookTimestampHeader,
		),
	)

	signature := strings.TrimSpace(
		request.Header.Get(
			telnyxWebhookSignatureHeader,
		),
	)

	if timestamp == "" ||
		signature == "" {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookUnauthorized
	}

	signedUnix, err := strconv.ParseInt(
		timestamp,
		10,
		64,
	)
	if err != nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookUnauthorized
	}

	signedAt := time.Unix(
		signedUnix,
		0,
	).UTC()

	now := d.now().UTC()

	if signedAt.Before(
		now.Add(
			-telnyxWebhookTimestampTolerance,
		),
	) ||
		signedAt.After(
			now.Add(
				telnyxWebhookTimestampTolerance,
			),
		) {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookUnauthorized
	}

	body, err := io.ReadAll(
		io.LimitReader(
			request.Body,
			telnyxWebhookMaxBodyBytes+1,
		),
	)
	if err != nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	if len(body) == 0 ||
		len(body) > telnyxWebhookMaxBodyBytes {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	signatureBytes, err :=
		base64.StdEncoding.DecodeString(
			signature,
		)
	if err != nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookUnauthorized
	}

	signedPayload := make(
		[]byte,
		0,
		len(timestamp)+1+len(body),
	)

	signedPayload = append(
		signedPayload,
		timestamp...,
	)

	signedPayload = append(
		signedPayload,
		'|',
	)

	signedPayload = append(
		signedPayload,
		body...,
	)

	if !ed25519.Verify(
		d.publicKey,
		signedPayload,
		signatureBytes,
	) {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookUnauthorized
	}

	var webhook telnyxDeliveryWebhook

	if err := json.Unmarshal(
		body,
		&webhook,
	); err != nil {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	eventType := strings.TrimSpace(
		webhook.Data.EventType,
	)

	providerMessageID := strings.TrimSpace(
		webhook.Data.Payload.ID,
	)

	if eventType != "message.sent" &&
		eventType != "message.finalized" {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookIgnored
	}

	if providerMessageID == "" ||
		webhook.Data.OccurredAt.IsZero() {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	switch eventType {
	case "message.sent":
		return DeliveryReceiptInput{
			Provider:          DeliveryTrackingProviderTelnyx,
			ProviderMessageID: providerMessageID,
			Status:            DeliveryReceiptStatusSent,
			ProviderStatus:    "sent",
			OccurredAt:        webhook.Data.OccurredAt.UTC(),
		}, nil

	case "message.finalized":
		return d.decodeFinalized(
			webhook,
		)

	default:
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookIgnored
	}
}

func (d *TelnyxDeliveryReceiptDecoder) decodeFinalized(
	webhook telnyxDeliveryWebhook,
) (DeliveryReceiptInput, error) {
	if len(webhook.Data.Payload.To) == 0 {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	providerStatus := strings.ToLower(
		strings.TrimSpace(
			webhook.Data.Payload.To[0].Status,
		),
	)

	if providerStatus == "" {
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookInvalid
	}

	receipt := DeliveryReceiptInput{
		Provider: DeliveryTrackingProviderTelnyx,
		ProviderMessageID: strings.TrimSpace(
			webhook.Data.Payload.ID,
		),
		ProviderStatus: providerStatus,
		OccurredAt:     webhook.Data.OccurredAt.UTC(),
	}

	switch providerStatus {
	case "delivered":
		receipt.Status =
			DeliveryReceiptStatusDelivered

		return receipt, nil

	case "sending_failed",
		"delivery_failed":
		receipt.Status =
			DeliveryReceiptStatusFailed

		receipt.FailureCode =
			telnyxFailureCode(
				webhook,
				providerStatus,
			)

		return receipt, nil

	case "sent":
		receipt.Status =
			DeliveryReceiptStatusSent

		return receipt, nil

	case "queued",
		"sending",
		"delivery_unconfirmed":
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookIgnored

	default:
		return DeliveryReceiptInput{},
			ErrDeliveryWebhookIgnored
	}
}

func telnyxFailureCode(
	webhook telnyxDeliveryWebhook,
	fallback string,
) string {
	if len(webhook.Data.Payload.Errors) > 0 {
		code := strings.TrimSpace(
			webhook.Data.Payload.Errors[0].Code,
		)

		if code != "" {
			return code
		}
	}

	return fallback
}
