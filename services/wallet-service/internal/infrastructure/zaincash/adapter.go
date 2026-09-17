package zaincash

import (
	"context"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
)

// Adapter implements topup.ZainCashClient by translating between this
// package's own request/response types and topup's domain types, so
// the topup package doesn't depend on this HTTP/JWT client directly.
type Adapter struct {
	client *Client
}

func NewAdapter(client *Client) *Adapter {
	if client == nil {
		panic("zaincash client is required")
	}

	return &Adapter{client: client}
}

func (a *Adapter) InitTransaction(
	ctx context.Context,
	input topup.InitTransactionInput,
) (topup.InitTransactionResult, error) {
	result, err := a.client.InitTransaction(ctx, InitTransactionInput{
		Language:             input.Language,
		ExternalReferenceID:  input.ExternalReferenceID,
		OrderID:              input.OrderID,
		ServiceType:          input.ServiceType,
		AmountValue:          input.AmountValue,
		CustomerPhone:        input.CustomerPhone,
		SuccessURL:           input.SuccessURL,
		FailureURL:           input.FailureURL,
	})
	if err != nil {
		return topup.InitTransactionResult{}, err
	}

	return topup.InitTransactionResult{
		TransactionID: result.TransactionID,
		RedirectURL:   result.RedirectURL,
	}, nil
}

func (a *Adapter) VerifyToken(tokenString string) (topup.WebhookEvent, error) {
	claims, err := a.client.VerifyToken(tokenString)
	if err != nil {
		return topup.WebhookEvent{}, err
	}

	return topup.WebhookEvent{
		EventID:             claims.EventID,
		EventType:           claims.EventType,
		TransactionID:       claims.TransactionID,
		MerchantReferenceID: claims.MerchantReferenceID,
		OrderID:             claims.OrderID,
		CurrentStatus:       claims.CurrentStatus,
		PreviousStatus:      claims.PreviousStatus,
	}, nil
}

// Compile-time proof that this adapter satisfies the port.
var _ topup.ZainCashClient = (*Adapter)(nil)
