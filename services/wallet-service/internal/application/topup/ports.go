package topup

import "context"

type Repository interface {
	Create(ctx context.Context, t TopUp) (TopUp, error)

	FindByExternalReferenceID(
		ctx context.Context,
		externalReferenceID string,
	) (TopUp, error)

	SetZainCashTransactionID(
		ctx context.Context,
		externalReferenceID string,
		zainCashTransactionID string,
	) (TopUp, error)

	MarkSucceeded(
		ctx context.Context,
		externalReferenceID string,
		zainCashTransactionID string,
	) (TopUp, error)

	MarkFailed(
		ctx context.Context,
		externalReferenceID string,
		reason string,
	) (TopUp, error)
}

// ZainCashClient is this package's view of the gateway — see
// infrastructure/zaincash for the real HTTP/JWT implementation, and
// its adapter for the translation between the two.
type ZainCashClient interface {
	InitTransaction(
		ctx context.Context,
		input InitTransactionInput,
	) (InitTransactionResult, error)

	VerifyToken(tokenString string) (WebhookEvent, error)
}
