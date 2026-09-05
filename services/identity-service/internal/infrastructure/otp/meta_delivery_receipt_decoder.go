package otp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	metaWebhookSignatureHeader = "X-Hub-Signature-256"
	metaWebhookMaxBodyBytes    = 256 * 1024
)

type MetaDeliveryReceiptDecoder struct {
	appSecret string
}

type metaDeliveryWebhook struct {
	Object string `json:"object"`

	Entry []struct {
		Changes []struct {
			Field string `json:"field"`

			Value struct {
				MessagingProduct string `json:"messaging_product"`

				Statuses []struct {
					ID        string `json:"id"`
					Status    string `json:"status"`
					Timestamp string `json:"timestamp"`

					Errors []struct {
						Code int64 `json:"code"`
					} `json:"errors"`
				} `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func NewMetaDeliveryReceiptDecoder(
	appSecret string,
) (*MetaDeliveryReceiptDecoder, error) {
	appSecret = strings.TrimSpace(
		appSecret,
	)

	if appSecret == "" {
		return nil, errors.New(
			"Meta app secret is required",
		)
	}

	return &MetaDeliveryReceiptDecoder{
		appSecret: appSecret,
	}, nil
}

func (d *MetaDeliveryReceiptDecoder) DecodeBatch(
	_ context.Context,
	request *http.Request,
) ([]DeliveryReceiptInput, error) {
	if d == nil ||
		strings.TrimSpace(d.appSecret) == "" {
		return nil, ErrDeliveryWebhookInvalid
	}

	if request == nil ||
		request.Body == nil {
		return nil, ErrDeliveryWebhookInvalid
	}

	signature := strings.TrimSpace(
		request.Header.Get(
			metaWebhookSignatureHeader,
		),
	)

	if signature == "" {
		return nil, ErrDeliveryWebhookUnauthorized
	}

	providedSignature, err :=
		decodeMetaWebhookSignature(
			signature,
		)
	if err != nil {
		return nil, ErrDeliveryWebhookUnauthorized
	}

	body, err := io.ReadAll(
		io.LimitReader(
			request.Body,
			metaWebhookMaxBodyBytes+1,
		),
	)
	if err != nil {
		return nil, ErrDeliveryWebhookInvalid
	}

	if len(body) == 0 ||
		len(body) > metaWebhookMaxBodyBytes {
		return nil, ErrDeliveryWebhookInvalid
	}

	expectedMAC := hmac.New(
		sha256.New,
		[]byte(d.appSecret),
	)

	if _, err := expectedMAC.Write(
		body,
	); err != nil {
		return nil, ErrDeliveryWebhookInvalid
	}

	expectedSignature :=
		expectedMAC.Sum(nil)

	if !hmac.Equal(
		providedSignature,
		expectedSignature,
	) {
		return nil, ErrDeliveryWebhookUnauthorized
	}

	var webhook metaDeliveryWebhook

	if err := json.Unmarshal(
		body,
		&webhook,
	); err != nil {
		return nil, ErrDeliveryWebhookInvalid
	}

	if strings.TrimSpace(
		webhook.Object,
	) != "whatsapp_business_account" {
		return nil, ErrDeliveryWebhookIgnored
	}

	receipts := make(
		[]DeliveryReceiptInput,
		0,
	)

	for _, entry := range webhook.Entry {
		for _, change := range entry.Changes {
			if strings.TrimSpace(
				change.Field,
			) != "messages" {
				continue
			}

			if strings.TrimSpace(
				change.Value.MessagingProduct,
			) != "whatsapp" {
				continue
			}

			for _, status := range change.Value.Statuses {
				receipt, include, err :=
					decodeMetaDeliveryStatus(
						status,
					)
				if err != nil {
					return nil, err
				}

				if !include {
					continue
				}

				receipts = append(
					receipts,
					receipt,
				)
			}
		}
	}

	if len(receipts) == 0 {
		return nil, ErrDeliveryWebhookIgnored
	}

	return receipts, nil
}

func decodeMetaWebhookSignature(
	signature string,
) ([]byte, error) {
	const prefix = "sha256="

	if !strings.HasPrefix(
		signature,
		prefix,
	) {
		return nil, errors.New(
			"invalid Meta webhook signature algorithm",
		)
	}

	encodedSignature := strings.TrimSpace(
		strings.TrimPrefix(
			signature,
			prefix,
		),
	)

	if encodedSignature == "" {
		return nil, errors.New(
			"Meta webhook signature is empty",
		)
	}

	decodedSignature, err :=
		hex.DecodeString(
			encodedSignature,
		)
	if err != nil {
		return nil, errors.New(
			"Meta webhook signature must be valid hex",
		)
	}

	if len(decodedSignature) != sha256.Size {
		return nil, errors.New(
			"Meta webhook signature has invalid length",
		)
	}

	return decodedSignature, nil
}

func decodeMetaDeliveryStatus(
	status struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`

		Errors []struct {
			Code int64 `json:"code"`
		} `json:"errors"`
	},
) (DeliveryReceiptInput, bool, error) {
	providerMessageID := strings.TrimSpace(
		status.ID,
	)

	providerStatus := strings.ToLower(
		strings.TrimSpace(
			status.Status,
		),
	)

	timestamp := strings.TrimSpace(
		status.Timestamp,
	)

	if providerMessageID == "" ||
		providerStatus == "" ||
		timestamp == "" {
		return DeliveryReceiptInput{},
			false,
			ErrDeliveryWebhookInvalid
	}

	occurredUnix, err := strconv.ParseInt(
		timestamp,
		10,
		64,
	)
	if err != nil ||
		occurredUnix < 0 {
		return DeliveryReceiptInput{},
			false,
			ErrDeliveryWebhookInvalid
	}

	receipt := DeliveryReceiptInput{
		Provider: DeliveryTrackingProviderMeta,

		ProviderMessageID: providerMessageID,

		ProviderStatus: providerStatus,

		OccurredAt: time.Unix(
			occurredUnix,
			0,
		).UTC(),
	}

	switch providerStatus {
	case "sent":
		receipt.Status =
			DeliveryReceiptStatusSent

		return receipt, true, nil

	case "delivered":
		receipt.Status =
			DeliveryReceiptStatusDelivered

		return receipt, true, nil

	case "failed":
		receipt.Status =
			DeliveryReceiptStatusFailed

		receipt.FailureCode =
			metaDeliveryFailureCode(
				status.Errors,
			)

		return receipt, true, nil

	case "read", "deleted":
		return DeliveryReceiptInput{},
			false,
			nil

	default:
		return DeliveryReceiptInput{},
			false,
			nil
	}
}

func metaDeliveryFailureCode(
	errors []struct {
		Code int64 `json:"code"`
	},
) string {
	if len(errors) > 0 &&
		errors[0].Code != 0 {
		return strconv.FormatInt(
			errors[0].Code,
			10,
		)
	}

	return "failed"
}
