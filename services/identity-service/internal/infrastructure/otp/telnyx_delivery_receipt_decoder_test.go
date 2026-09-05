package otp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTelnyxDeliveryReceiptDecoderDecodesSent(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.sent",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-123"
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	receipt, err := decoder.Decode(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"Decode() returned an error: %v",
			err,
		)
	}

	if receipt.Provider !=
		DeliveryTrackingProviderTelnyx {
		t.Fatalf(
			"provider = %q, want %q",
			receipt.Provider,
			DeliveryTrackingProviderTelnyx,
		)
	}

	if receipt.ProviderMessageID !=
		"message-123" {
		t.Fatalf(
			"provider message ID = %q, want %q",
			receipt.ProviderMessageID,
			"message-123",
		)
	}

	if receipt.Status !=
		DeliveryReceiptStatusSent {
		t.Fatalf(
			"status = %q, want %q",
			receipt.Status,
			DeliveryReceiptStatusSent,
		)
	}

	if receipt.ProviderStatus != "sent" {
		t.Fatalf(
			"provider status = %q, want %q",
			receipt.ProviderStatus,
			"sent",
		)
	}

	expectedOccurredAt := time.Date(
		2026,
		time.September,
		1,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	if !receipt.OccurredAt.Equal(
		expectedOccurredAt,
	) {
		t.Fatalf(
			"occurred at = %s, want %s",
			receipt.OccurredAt,
			expectedOccurredAt,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderDecodesDelivered(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.finalized",
			"occurred_at": "2026-09-01T00:00:01Z",
			"payload": {
				"id": "message-456",
				"to": [
					{
						"status": "delivered"
					}
				]
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	receipt, err := decoder.Decode(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"Decode() returned an error: %v",
			err,
		)
	}

	if receipt.Status !=
		DeliveryReceiptStatusDelivered {
		t.Fatalf(
			"status = %q, want %q",
			receipt.Status,
			DeliveryReceiptStatusDelivered,
		)
	}

	if receipt.ProviderStatus != "delivered" {
		t.Fatalf(
			"provider status = %q, want %q",
			receipt.ProviderStatus,
			"delivered",
		)
	}

	if receipt.ProviderMessageID !=
		"message-456" {
		t.Fatalf(
			"provider message ID = %q, want %q",
			receipt.ProviderMessageID,
			"message-456",
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderDecodesFailed(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.finalized",
			"occurred_at": "2026-09-01T00:00:02Z",
			"payload": {
				"id": "message-789",
				"to": [
					{
						"status": "delivery_failed"
					}
				],
				"errors": [
					{
						"code": "40001"
					}
				]
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	receipt, err := decoder.Decode(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"Decode() returned an error: %v",
			err,
		)
	}

	if receipt.Status !=
		DeliveryReceiptStatusFailed {
		t.Fatalf(
			"status = %q, want %q",
			receipt.Status,
			DeliveryReceiptStatusFailed,
		)
	}

	if receipt.ProviderStatus !=
		"delivery_failed" {
		t.Fatalf(
			"provider status = %q, want %q",
			receipt.ProviderStatus,
			"delivery_failed",
		)
	}

	if receipt.FailureCode != "40001" {
		t.Fatalf(
			"failure code = %q, want %q",
			receipt.FailureCode,
			"40001",
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderUsesStatusAsFailureCodeFallback(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.finalized",
			"occurred_at": "2026-09-01T00:00:02Z",
			"payload": {
				"id": "message-789",
				"to": [
					{
						"status": "sending_failed"
					}
				]
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	receipt, err := decoder.Decode(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"Decode() returned an error: %v",
			err,
		)
	}

	if receipt.FailureCode !=
		"sending_failed" {
		t.Fatalf(
			"failure code = %q, want %q",
			receipt.FailureCode,
			"sending_failed",
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderIgnoresUnrelatedEvent(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.received",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-inbound"
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookIgnored,
	) {
		t.Fatalf(
			"Decode() error = %v, want ignored error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderIgnoresDeliveryUnconfirmed(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.finalized",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-unconfirmed",
				"to": [
					{
						"status": "delivery_unconfirmed"
					}
				]
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookIgnored,
	) {
		t.Fatalf(
			"Decode() error = %v, want ignored error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderRejectsInvalidSignature(
	t *testing.T,
) {
	decoder, _, now := newTestTelnyxDecoder(
		t,
	)

	_, wrongPrivateKey, err :=
		ed25519.GenerateKey(
			rand.Reader,
		)
	if err != nil {
		t.Fatalf(
			"GenerateKey() returned an error: %v",
			err,
		)
	}

	body := `{
		"data": {
			"event_type": "message.sent",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-123"
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		wrongPrivateKey,
		now,
		body,
	)

	_, err = decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"Decode() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderRejectsExpiredTimestamp(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.sent",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-123"
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now.Add(
			-6*time.Minute,
		),
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"Decode() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderRejectsFutureTimestamp(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.sent",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {
				"id": "message-123"
			}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now.Add(
			6*time.Minute,
		),
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"Decode() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderRejectsMalformedPayload(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{not-json}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookInvalid,
	) {
		t.Fatalf(
			"Decode() error = %v, want invalid error",
			err,
		)
	}
}

func TestTelnyxDeliveryReceiptDecoderRejectsMissingMessageID(
	t *testing.T,
) {
	decoder, privateKey, now := newTestTelnyxDecoder(
		t,
	)

	body := `{
		"data": {
			"event_type": "message.sent",
			"occurred_at": "2026-09-01T00:00:00Z",
			"payload": {}
		}
	}`

	request := signedTelnyxWebhookRequest(
		t,
		privateKey,
		now,
		body,
	)

	_, err := decoder.Decode(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookInvalid,
	) {
		t.Fatalf(
			"Decode() error = %v, want invalid error",
			err,
		)
	}
}

func TestNewTelnyxDeliveryReceiptDecoderRejectsInvalidPublicKey(
	t *testing.T,
) {
	tests := []struct {
		name      string
		publicKey string
	}{
		{
			name:      "blank",
			publicKey: "",
		},
		{
			name:      "not base64",
			publicKey: "***",
		},
		{
			name: "wrong key size",
			publicKey: base64.StdEncoding.EncodeToString(
				[]byte("too-short"),
			),
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				decoder, err :=
					NewTelnyxDeliveryReceiptDecoder(
						testCase.publicKey,
					)

				if err == nil {
					t.Fatal(
						"NewTelnyxDeliveryReceiptDecoder() accepted invalid public key",
					)
				}

				if decoder != nil {
					t.Fatal(
						"NewTelnyxDeliveryReceiptDecoder() returned decoder for invalid public key",
					)
				}
			},
		)
	}
}

func newTestTelnyxDecoder(
	t *testing.T,
) (
	*TelnyxDeliveryReceiptDecoder,
	ed25519.PrivateKey,
	time.Time,
) {
	t.Helper()

	publicKey, privateKey, err :=
		ed25519.GenerateKey(
			rand.Reader,
		)
	if err != nil {
		t.Fatalf(
			"GenerateKey() returned an error: %v",
			err,
		)
	}

	decoder, err :=
		NewTelnyxDeliveryReceiptDecoder(
			base64.StdEncoding.EncodeToString(
				publicKey,
			),
		)
	if err != nil {
		t.Fatalf(
			"NewTelnyxDeliveryReceiptDecoder() returned an error: %v",
			err,
		)
	}

	now := time.Date(
		2026,
		time.September,
		1,
		0,
		2,
		0,
		0,
		time.UTC,
	)

	decoder.now = func() time.Time {
		return now
	}

	return decoder, privateKey, now
}

func signedTelnyxWebhookRequest(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	timestamp time.Time,
	body string,
) *http.Request {
	t.Helper()

	timestampValue := strconv.FormatInt(
		timestamp.Unix(),
		10,
	)

	signedPayload := []byte(
		timestampValue +
			"|" +
			body,
	)

	signature := ed25519.Sign(
		privateKey,
		signedPayload,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/telnyx",
		strings.NewReader(
			body,
		),
	)

	request.Header.Set(
		telnyxWebhookTimestampHeader,
		timestampValue,
	)

	request.Header.Set(
		telnyxWebhookSignatureHeader,
		base64.StdEncoding.EncodeToString(
			signature,
		),
	)

	return request
}
