package zaincash

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
)

// Adapter is ZainCash as a topup.Provider: it opens a payment session on
// ZainCash's page and reads ZainCash's signed notifications, translating
// between this client's types and topup's.
type Adapter struct {
	client *Client
}

func NewAdapter(client *Client) *Adapter {
	if client == nil {
		panic("zaincash client is required")
	}

	return &Adapter{client: client}
}

// ZainCash's final transaction statuses; every other one (PENDING,
// OTP_SENT, CUSTOMER_AUTHENTICATION_REQUIRED, EXPIRED, ...) is not final yet.
const (
	statusSuccess = "SUCCESS"
	statusFailed  = "FAILED"
)

func (a *Adapter) Start(ctx context.Context, input topup.StartInput) (topup.StartResult, error) {
	// ZainCash takes whole dinars only.
	if !input.Amount.Equal(input.Amount.Truncate(0)) {
		return topup.StartResult{}, topup.ErrAmountNotSupported
	}

	result, err := a.client.InitTransaction(ctx, InitTransactionInput{
		Language:            "ar",
		ExternalReferenceID: input.ReferenceID,
		OrderID:             input.OrderID,
		ServiceType:         fmt.Sprintf("%s_topup", input.OwnerType),
		AmountValue:         input.Amount.StringFixed(0),
		SuccessURL:          input.SuccessURL,
		FailureURL:          input.FailureURL,
	})
	if err != nil {
		return topup.StartResult{}, err
	}

	return topup.StartResult{ProviderTransactionID: result.TransactionID, RedirectURL: result.RedirectURL}, nil
}

func (a *Adapter) Verify(token string) (topup.Notice, error) {
	claims, err := a.client.VerifyToken(token)
	if err != nil {
		return topup.Notice{}, err
	}

	outcome := topup.OutcomePending

	switch claims.CurrentStatus {
	case statusSuccess:
		outcome = topup.OutcomeSucceeded
	case statusFailed:
		outcome = topup.OutcomeFailed
	}

	return topup.Notice{
		ReferenceID:           claims.MerchantReferenceID,
		ProviderTransactionID: claims.TransactionID,
		Outcome:               outcome,
	}, nil
}

// Compile-time proof that this adapter is a provider.
var _ topup.Provider = (*Adapter)(nil)
