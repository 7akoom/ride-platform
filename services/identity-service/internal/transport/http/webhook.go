package httptransport

import (
	"context"
	"errors"
	"net/http"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type DeliveryReceiptDecoder interface {
	Decode(
		ctx context.Context,
		request *http.Request,
	) (otp.DeliveryReceiptInput, error)
}

type DeliveryReceiptBatchDecoder interface {
	DecodeBatch(
		ctx context.Context,
		request *http.Request,
	) ([]otp.DeliveryReceiptInput, error)
}

type DeliveryReceiptHandler struct {
	decode func(
		context.Context,
		*http.Request,
	) ([]otp.DeliveryReceiptInput, error)

	store    otp.DeliveryReceiptStore
	provider otp.DeliveryTrackingProvider
	metrics  DeliveryWebhookMetricsRecorder
}

func NewDeliveryReceiptHandler(
	decoder DeliveryReceiptDecoder,
	store otp.DeliveryReceiptStore,
	options ...DeliveryReceiptHandlerOption,
) (*DeliveryReceiptHandler, error) {
	if decoder == nil {
		return nil, errors.New(
			"delivery receipt decoder is required",
		)
	}

	if store == nil {
		return nil, errors.New(
			"delivery receipt store is required",
		)
	}

	return (&DeliveryReceiptHandler{
		decode: func(
			ctx context.Context,
			request *http.Request,
		) ([]otp.DeliveryReceiptInput, error) {
			receipt, err := decoder.Decode(
				ctx,
				request,
			)
			if err != nil {
				return nil, err
			}

			return []otp.DeliveryReceiptInput{
				receipt,
			}, nil
		},
		store: store,
	}).configure(options)
}

func NewDeliveryReceiptBatchHandler(
	decoder DeliveryReceiptBatchDecoder,
	store otp.DeliveryReceiptStore,
	options ...DeliveryReceiptHandlerOption,
) (*DeliveryReceiptHandler, error) {
	if decoder == nil {
		return nil, errors.New(
			"delivery receipt batch decoder is required",
		)
	}

	if store == nil {
		return nil, errors.New(
			"delivery receipt store is required",
		)
	}

	return (&DeliveryReceiptHandler{
		decode: decoder.DecodeBatch,
		store:  store,
	}).configure(options)
}

func (h *DeliveryReceiptHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if h == nil ||
		h.decode == nil ||
		h.store == nil {
		http.Error(
			writer,
			http.StatusText(
				http.StatusInternalServerError,
			),
			http.StatusInternalServerError,
		)

		return
	}

	receipts, err := h.decode(
		request.Context(),
		request,
	)
	if err != nil {
		h.recordOutcome(request.Context(), deliveryWebhookDecodeOutcome(err))
		writeDeliveryWebhookDecodeError(
			writer,
			err,
		)

		return
	}

	outcome := DeliveryWebhookAccepted
	if len(receipts) == 0 {
		outcome = DeliveryWebhookIgnored
	}
	for _, receipt := range receipts {
		if err := h.store.ApplyReceipt(
			request.Context(),
			receipt,
		); err != nil {
			h.recordOutcome(request.Context(), DeliveryWebhookPersistenceFailed)
			http.Error(
				writer,
				http.StatusText(
					http.StatusInternalServerError,
				),
				http.StatusInternalServerError,
			)

			return
		}
	}

	h.recordOutcome(request.Context(), outcome)
	writer.WriteHeader(
		http.StatusNoContent,
	)
}

func writeDeliveryWebhookDecodeError(
	writer http.ResponseWriter,
	err error,
) {
	switch {
	case errors.Is(
		err,
		otp.ErrDeliveryWebhookIgnored,
	):
		writer.WriteHeader(
			http.StatusNoContent,
		)

	case errors.Is(
		err,
		otp.ErrDeliveryWebhookUnauthorized,
	):
		http.Error(
			writer,
			http.StatusText(
				http.StatusForbidden,
			),
			http.StatusForbidden,
		)

	case errors.Is(
		err,
		otp.ErrDeliveryWebhookInvalid,
	):
		http.Error(
			writer,
			http.StatusText(
				http.StatusBadRequest,
			),
			http.StatusBadRequest,
		)

	default:
		http.Error(
			writer,
			http.StatusText(
				http.StatusBadRequest,
			),
			http.StatusBadRequest,
		)
	}
}
