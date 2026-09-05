package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type testDeliveryReceiptDecoder struct {
	receipt otp.DeliveryReceiptInput
	err     error
	calls   int
}

func (d *testDeliveryReceiptDecoder) Decode(
	ctx context.Context,
	request *http.Request,
) (otp.DeliveryReceiptInput, error) {
	d.calls++

	return d.receipt, d.err
}

type testDeliveryReceiptStore struct {
	input otp.DeliveryReceiptInput
	err   error
	calls int
}

func (s *testDeliveryReceiptStore) ApplyReceipt(
	ctx context.Context,
	input otp.DeliveryReceiptInput,
) error {
	s.calls++
	s.input = input

	return s.err
}

func TestNewDeliveryReceiptHandlerRejectsNilDecoder(
	t *testing.T,
) {
	_, err := NewDeliveryReceiptHandler(
		nil,
		&testDeliveryReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"NewDeliveryReceiptHandler() accepted nil decoder",
		)
	}
}

func TestNewDeliveryReceiptHandlerRejectsNilStore(
	t *testing.T,
) {
	_, err := NewDeliveryReceiptHandler(
		&testDeliveryReceiptDecoder{},
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewDeliveryReceiptHandler() accepted nil store",
		)
	}
}

func TestDeliveryReceiptHandlerAppliesDecodedReceipt(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		31,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	expectedReceipt := otp.DeliveryReceiptInput{
		Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
		ProviderMessageID: "request-123",
		Status:            otp.DeliveryReceiptStatusDelivered,
		ProviderStatus:    "delivered",
		OccurredAt:        occurredAt,
	}

	decoder := &testDeliveryReceiptDecoder{
		receipt: expectedReceipt,
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if decoder.calls != 1 {
		t.Fatalf(
			"decoder calls = %d, want 1",
			decoder.calls,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"store calls = %d, want 1",
			store.calls,
		)
	}

	if store.input != expectedReceipt {
		t.Fatalf(
			"stored receipt = %+v, want %+v",
			store.input,
			expectedReceipt,
		)
	}
}

func TestDeliveryReceiptHandlerReturnsBadRequestForDecodeFailure(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptDecoder{
		err: errors.New(
			"invalid webhook payload",
		),
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if decoder.calls != 1 {
		t.Fatalf(
			"decoder calls = %d, want 1",
			decoder.calls,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}

func TestDeliveryReceiptHandlerReturnsInternalServerErrorForStoreFailure(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptDecoder{
		receipt: otp.DeliveryReceiptInput{
			Provider:          otp.DeliveryTrackingProviderBulkSMSIraq,
			ProviderMessageID: "request-123",
			Status:            otp.DeliveryReceiptStatusDelivered,
			ProviderStatus:    "delivered",
			OccurredAt: time.Date(
				2026,
				time.August,
				31,
				20,
				0,
				0,
				0,
				time.UTC,
			),
		},
	}

	store := &testDeliveryReceiptStore{
		err: errors.New(
			"database unavailable",
		),
	}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusInternalServerError {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}

	if decoder.calls != 1 {
		t.Fatalf(
			"decoder calls = %d, want 1",
			decoder.calls,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"store calls = %d, want 1",
			store.calls,
		)
	}
}

func TestDeliveryReceiptHandlerRejectsUnconfiguredHandler(
	t *testing.T,
) {
	var handler *DeliveryReceiptHandler

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusInternalServerError {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}
}
func TestDeliveryReceiptHandlerReturnsForbiddenForUnauthorizedWebhook(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptDecoder{
		err: otp.ErrDeliveryWebhookUnauthorized,
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/telnyx",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}

func TestDeliveryReceiptHandlerReturnsNoContentForIgnoredWebhook(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptDecoder{
		err: otp.ErrDeliveryWebhookIgnored,
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/telnyx",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}

func TestDeliveryReceiptHandlerReturnsBadRequestForInvalidWebhook(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptDecoder{
		err: otp.ErrDeliveryWebhookInvalid,
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/telnyx",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}

type testDeliveryReceiptBatchDecoder struct {
	receipts []otp.DeliveryReceiptInput
	err      error
}

func (d *testDeliveryReceiptBatchDecoder) DecodeBatch(
	_ context.Context,
	_ *http.Request,
) ([]otp.DeliveryReceiptInput, error) {
	return d.receipts, d.err
}

func TestNewDeliveryReceiptBatchHandlerRejectsNilDecoder(
	t *testing.T,
) {
	handler, err := NewDeliveryReceiptBatchHandler(
		nil,
		&testDeliveryReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"NewDeliveryReceiptBatchHandler() accepted nil decoder",
		)
	}

	if handler != nil {
		t.Fatal(
			"NewDeliveryReceiptBatchHandler() returned handler for nil decoder",
		)
	}
}

func TestNewDeliveryReceiptBatchHandlerRejectsNilStore(
	t *testing.T,
) {
	handler, err := NewDeliveryReceiptBatchHandler(
		&testDeliveryReceiptBatchDecoder{},
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewDeliveryReceiptBatchHandler() accepted nil store",
		)
	}

	if handler != nil {
		t.Fatal(
			"NewDeliveryReceiptBatchHandler() returned handler for nil store",
		)
	}
}

func TestDeliveryReceiptBatchHandlerAppliesAllReceipts(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptBatchDecoder{
		receipts: []otp.DeliveryReceiptInput{
			{
				Provider:          otp.DeliveryTrackingProviderMeta,
				ProviderMessageID: "wamid-1",
				Status:            otp.DeliveryReceiptStatusSent,
				ProviderStatus:    "sent",
			},
			{
				Provider:          otp.DeliveryTrackingProviderMeta,
				ProviderMessageID: "wamid-2",
				Status:            otp.DeliveryReceiptStatusDelivered,
				ProviderStatus:    "delivered",
			},
		},
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptBatchHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptBatchHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if store.calls != 2 {
		t.Fatalf(
			"store calls = %d, want 2",
			store.calls,
		)
	}
}

func TestDeliveryReceiptBatchHandlerReturnsNoContentForEmptyBatch(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptBatchDecoder{
		receipts: []otp.DeliveryReceiptInput{},
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptBatchHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptBatchHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}

func TestDeliveryReceiptBatchHandlerReturnsBadRequestForDecodeFailure(
	t *testing.T,
) {
	decoder := &testDeliveryReceiptBatchDecoder{
		err: otp.ErrDeliveryWebhookInvalid,
	}

	store := &testDeliveryReceiptStore{}

	handler, err := NewDeliveryReceiptBatchHandler(
		decoder,
		store,
	)
	if err != nil {
		t.Fatalf(
			"NewDeliveryReceiptBatchHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.calls,
		)
	}
}
