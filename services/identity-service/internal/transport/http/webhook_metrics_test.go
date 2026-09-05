package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type testWebhookMetrics struct {
	calls    int
	provider otp.DeliveryTrackingProvider
	outcome  DeliveryWebhookOutcome
}

func (m *testWebhookMetrics) RecordDeliveryWebhook(_ context.Context, provider otp.DeliveryTrackingProvider, outcome DeliveryWebhookOutcome) {
	m.calls++
	m.provider, m.outcome = provider, outcome
}

type receiptSequenceStore struct{ calls, failAt int }

func (s *receiptSequenceStore) ApplyReceipt(context.Context, otp.DeliveryReceiptInput) error {
	s.calls++
	if s.calls == s.failAt {
		return errors.New("persistence unavailable")
	}
	return nil
}

func TestWebhookMetricsProcessingOutcomes(t *testing.T) {
	for _, provider := range []otp.DeliveryTrackingProvider{otp.DeliveryTrackingProviderTelnyx, otp.DeliveryTrackingProviderMeta} {
		for _, tc := range []struct {
			name          string
			decodeErr     error
			failAt        int
			want          DeliveryWebhookOutcome
			status, calls int
		}{
			{"accepted", nil, 0, DeliveryWebhookAccepted, 204, 1},
			{"ignored", otp.ErrDeliveryWebhookIgnored, 0, DeliveryWebhookIgnored, 204, 0},
			{"unauthorized", otp.ErrDeliveryWebhookUnauthorized, 0, DeliveryWebhookUnauthorized, 403, 0},
			{"invalid", otp.ErrDeliveryWebhookInvalid, 0, DeliveryWebhookInvalid, 400, 0},
			{"unknown_decode_error", errors.New("decoder failure"), 0, DeliveryWebhookInvalid, 400, 0},
			{"persistence_failed", nil, 1, DeliveryWebhookPersistenceFailed, 500, 1},
		} {
			t.Run(string(provider)+"/"+tc.name, func(t *testing.T) {
				m := &testWebhookMetrics{}
				store := &receiptSequenceStore{failAt: tc.failAt}
				options := []DeliveryReceiptHandlerOption{WithDeliveryWebhookMetrics(provider, m)}
				var handler *DeliveryReceiptHandler
				var err error
				if provider == otp.DeliveryTrackingProviderTelnyx {
					handler, err = NewDeliveryReceiptHandler(&testDeliveryReceiptDecoder{err: tc.decodeErr}, store, options...)
				} else {
					handler, err = NewDeliveryReceiptBatchHandler(&testDeliveryReceiptBatchDecoder{receipts: []otp.DeliveryReceiptInput{{}}, err: tc.decodeErr}, store, options...)
				}
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
				if response.Code != tc.status || store.calls != tc.calls {
					t.Fatal("unexpected response or persistence calls")
				}
				if m.calls != 1 || m.provider != provider || m.outcome != tc.want {
					t.Fatal("expected one metric for the processing outcome")
				}
			})
		}
	}
}

func TestWebhookBatchMetricsEmptyAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		receipts int
		failAt   int
		outcome  DeliveryWebhookOutcome
		status   int
	}{
		{"empty", 0, 0, DeliveryWebhookIgnored, 204},
		{"accepted_batch", 3, 0, DeliveryWebhookAccepted, 204},
		{"partial_failure", 3, 2, DeliveryWebhookPersistenceFailed, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &testWebhookMetrics{}
			store := &receiptSequenceStore{failAt: tc.failAt}
			handler, err := NewDeliveryReceiptBatchHandler(&testDeliveryReceiptBatchDecoder{receipts: make([]otp.DeliveryReceiptInput, tc.receipts)}, store,
				WithDeliveryWebhookMetrics(otp.DeliveryTrackingProviderMeta, m))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
			if response.Code != tc.status || m.calls != 1 || m.outcome != tc.outcome {
				t.Fatal("incorrect batch outcome")
			}
			wantCalls := tc.receipts
			if tc.failAt != 0 {
				wantCalls = tc.failAt
			}
			if store.calls != wantCalls {
				t.Fatal("incorrect number of persistence calls")
			}
		})
	}
}
