package otp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testMetaAppSecret = "meta-test-app-secret"

func TestMetaDeliveryReceiptDecoderDecodesBatch(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-1",
									"status": "sent",
									"timestamp": "1788220800"
								},
								{
									"id": "wamid-2",
									"status": "delivered",
									"timestamp": "1788220802"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	receipts, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"DecodeBatch() returned an error: %v",
			err,
		)
	}

	if len(receipts) != 2 {
		t.Fatalf(
			"receipt count = %d, want 2",
			len(receipts),
		)
	}

	if receipts[0].Provider !=
		DeliveryTrackingProviderMeta {
		t.Fatalf(
			"first provider = %q, want %q",
			receipts[0].Provider,
			DeliveryTrackingProviderMeta,
		)
	}

	if receipts[0].ProviderMessageID !=
		"wamid-1" {
		t.Fatalf(
			"first provider message ID = %q, want %q",
			receipts[0].ProviderMessageID,
			"wamid-1",
		)
	}

	if receipts[0].Status !=
		DeliveryReceiptStatusSent {
		t.Fatalf(
			"first status = %q, want %q",
			receipts[0].Status,
			DeliveryReceiptStatusSent,
		)
	}

	if receipts[1].ProviderMessageID !=
		"wamid-2" {
		t.Fatalf(
			"second provider message ID = %q, want %q",
			receipts[1].ProviderMessageID,
			"wamid-2",
		)
	}

	if receipts[1].Status !=
		DeliveryReceiptStatusDelivered {
		t.Fatalf(
			"second status = %q, want %q",
			receipts[1].Status,
			DeliveryReceiptStatusDelivered,
		)
	}
}

func TestMetaDeliveryReceiptDecoderPreservesProviderTimestamps(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-newer",
									"status": "delivered",
									"timestamp": "1788220810"
								},
								{
									"id": "wamid-older",
									"status": "sent",
									"timestamp": "1788220800"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	receipts, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"DecodeBatch() returned an error: %v",
			err,
		)
	}

	if len(receipts) != 2 {
		t.Fatalf(
			"receipt count = %d, want 2",
			len(receipts),
		)
	}

	expectedNewer := time.Unix(
		1788220810,
		0,
	).UTC()

	expectedOlder := time.Unix(
		1788220800,
		0,
	).UTC()

	if !receipts[0].OccurredAt.Equal(
		expectedNewer,
	) {
		t.Fatalf(
			"first occurred at = %s, want %s",
			receipts[0].OccurredAt,
			expectedNewer,
		)
	}

	if !receipts[1].OccurredAt.Equal(
		expectedOlder,
	) {
		t.Fatalf(
			"second occurred at = %s, want %s",
			receipts[1].OccurredAt,
			expectedOlder,
		)
	}
}

func TestMetaDeliveryReceiptDecoderDecodesFailed(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-failed",
									"status": "failed",
									"timestamp": "1788220800",
									"errors": [
										{
											"code": 131026
										}
									]
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	receipts, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"DecodeBatch() returned an error: %v",
			err,
		)
	}

	if len(receipts) != 1 {
		t.Fatalf(
			"receipt count = %d, want 1",
			len(receipts),
		)
	}

	receipt := receipts[0]

	if receipt.Status !=
		DeliveryReceiptStatusFailed {
		t.Fatalf(
			"status = %q, want %q",
			receipt.Status,
			DeliveryReceiptStatusFailed,
		)
	}

	if receipt.ProviderStatus != "failed" {
		t.Fatalf(
			"provider status = %q, want %q",
			receipt.ProviderStatus,
			"failed",
		)
	}

	if receipt.FailureCode != "131026" {
		t.Fatalf(
			"failure code = %q, want %q",
			receipt.FailureCode,
			"131026",
		)
	}
}

func TestMetaDeliveryReceiptDecoderUsesFailureFallback(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-failed",
									"status": "failed",
									"timestamp": "1788220800"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	receipts, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"DecodeBatch() returned an error: %v",
			err,
		)
	}

	if receipts[0].FailureCode != "failed" {
		t.Fatalf(
			"failure code = %q, want %q",
			receipts[0].FailureCode,
			"failed",
		)
	}
}

func TestMetaDeliveryReceiptDecoderIgnoresReadAndDeleted(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-read",
									"status": "read",
									"timestamp": "1788220800"
								},
								{
									"id": "wamid-deleted",
									"status": "deleted",
									"timestamp": "1788220801"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookIgnored,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want ignored error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderIgnoresNonStatusWebhook(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"messages": [
								{
									"id": "wamid-inbound"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookIgnored,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want ignored error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsInvalidSignature(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": []
	}`

	request := signedMetaWebhookRequest(
		body,
		"wrong-secret",
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsMissingSignature(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		strings.NewReader(
			`{"object":"whatsapp_business_account"}`,
		),
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsMalformedSignature(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		strings.NewReader(
			`{"object":"whatsapp_business_account"}`,
		),
	)

	request.Header.Set(
		metaWebhookSignatureHeader,
		"sha256=not-hex",
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookUnauthorized,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want unauthorized error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsMalformedPayload(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{not-json}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookInvalid,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want invalid error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsMissingMessageID(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"status": "sent",
									"timestamp": "1788220800"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookInvalid,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want invalid error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderRejectsInvalidTimestamp(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "wamid-1",
									"status": "sent",
									"timestamp": "not-unix"
								}
							]
						}
					}
				]
			}
		]
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookInvalid,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want invalid error",
			err,
		)
	}
}

func TestMetaDeliveryReceiptDecoderIgnoresWrongObject(
	t *testing.T,
) {
	decoder := newTestMetaDeliveryReceiptDecoder(
		t,
	)

	body := `{
		"object": "page",
		"entry": []
	}`

	request := signedMetaWebhookRequest(
		body,
		testMetaAppSecret,
	)

	_, err := decoder.DecodeBatch(
		context.Background(),
		request,
	)

	if !errors.Is(
		err,
		ErrDeliveryWebhookIgnored,
	) {
		t.Fatalf(
			"DecodeBatch() error = %v, want ignored error",
			err,
		)
	}
}

func TestNewMetaDeliveryReceiptDecoderRejectsBlankSecret(
	t *testing.T,
) {
	tests := []string{
		"",
		" ",
		"\t",
	}

	for _, appSecret := range tests {
		decoder, err :=
			NewMetaDeliveryReceiptDecoder(
				appSecret,
			)

		if err == nil {
			t.Fatalf(
				"NewMetaDeliveryReceiptDecoder(%q) accepted blank secret",
				appSecret,
			)
		}

		if decoder != nil {
			t.Fatalf(
				"NewMetaDeliveryReceiptDecoder(%q) returned decoder",
				appSecret,
			)
		}
	}
}

func newTestMetaDeliveryReceiptDecoder(
	t *testing.T,
) *MetaDeliveryReceiptDecoder {
	t.Helper()

	decoder, err :=
		NewMetaDeliveryReceiptDecoder(
			testMetaAppSecret,
		)
	if err != nil {
		t.Fatalf(
			"NewMetaDeliveryReceiptDecoder() returned an error: %v",
			err,
		)
	}

	return decoder
}

func signedMetaWebhookRequest(
	body string,
	appSecret string,
) *http.Request {
	mac := hmac.New(
		sha256.New,
		[]byte(appSecret),
	)

	_, _ = mac.Write(
		[]byte(body),
	)

	signature :=
		"sha256=" +
			hex.EncodeToString(
				mac.Sum(nil),
			)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		strings.NewReader(
			body,
		),
	)

	request.Header.Set(
		metaWebhookSignatureHeader,
		signature,
	)

	return request
}
