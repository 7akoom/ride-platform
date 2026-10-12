package zaincash

import (
	"context"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
)

// Adapter is ZainCash as a topup.Provider: it opens a payment session on
// ZainCash's page, asks ZainCash where a payment stands, and reads
// ZainCash's signed notifications, translating between this client's types
// and topup's.
type Adapter struct {
	client *Client
}

func NewAdapter(client *Client) *Adapter {
	if client == nil {
		panic("zaincash client is required")
	}

	return &Adapter{client: client}
}

// ZainCash's transaction statuses (docs: API Parameters & Statuses).
const (
	statusSuccess  = "SUCCESS"
	statusFailed   = "FAILED"
	statusExpired  = "EXPIRED"
	statusRefunded = "REFUNDED"
)

// outcomeOf is where a ZainCash status leaves a top-up. SUCCESS is the only
// one that moves money in. FAILED and EXPIRED end it unpaid; REFUNDED means
// the money went back to the customer, so it is never credited either.
// PENDING, OTP_SENT and CUSTOMER_AUTHENTICATION_REQUIRED (and anything new)
// are not final yet.
func outcomeOf(status string) topup.Outcome {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case statusSuccess:
		return topup.OutcomeSucceeded
	case statusFailed, statusExpired, statusRefunded:
		return topup.OutcomeFailed
	default:
		return topup.OutcomePending
	}
}

// languageOf is a language ZainCash's page speaks: ar, ku or en; Arabic
// otherwise.
func languageOf(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en":
		return "en"
	case "ku", "ckb":
		return "ku"
	default:
		return "ar"
	}
}

func (a *Adapter) Start(ctx context.Context, input topup.StartInput) (topup.StartResult, error) {
	// ZainCash takes whole dinars only.
	if !input.Amount.Equal(input.Amount.Truncate(0)) {
		return topup.StartResult{}, topup.ErrAmountNotSupported
	}

	result, err := a.client.InitTransaction(ctx, InitTransactionInput{
		Language:            languageOf(input.Language),
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

func (a *Adapter) Check(ctx context.Context, providerTransactionID string) (topup.Notice, error) {
	inquiry, err := a.client.Inquire(ctx, providerTransactionID)
	if err != nil {
		return topup.Notice{}, err
	}

	return topup.Notice{
		ReferenceID:           inquiry.ExternalReferenceID,
		ProviderTransactionID: providerTransactionID,
		Outcome:               outcomeOf(inquiry.Status),
		Detail:                "zaincash: " + inquiry.Status,
	}, nil
}

func (a *Adapter) Verify(token string) (topup.Notice, error) {
	data, err := a.client.VerifyToken(token)
	if err != nil {
		return topup.Notice{}, err
	}

	detail := "zaincash: " + data.CurrentStatus
	if data.ErrorMessage != "" {
		detail += ": " + data.ErrorMessage
	}

	return topup.Notice{
		ReferenceID:           data.MerchantReferenceID,
		ProviderTransactionID: data.TransactionID,
		Outcome:               outcomeOf(data.CurrentStatus),
		Detail:                detail,
	}, nil
}

// Compile-time proof that this adapter is a provider.
var _ topup.Provider = (*Adapter)(nil)
