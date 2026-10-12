package topup

import (
	"context"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type Repository interface {
	Create(ctx context.Context, t TopUp) (TopUp, error)
	FindByExternalReferenceID(ctx context.Context, externalReferenceID string) (TopUp, error)
	// FindForOwner is the owner's top-up; ErrTopUpNotFound for anyone else's.
	FindForOwner(ctx context.Context, ownerType wallet.OwnerType, ownerID, id string) (TopUp, error)
	// ListPending is the top-ups still pending that were created between
	// since and before, oldest first, at most limit of them.
	ListPending(ctx context.Context, since, before time.Time, limit int) ([]TopUp, error)
	SetProviderTransactionID(ctx context.Context, externalReferenceID, providerTransactionID string) (TopUp, error)
	MarkSucceeded(ctx context.Context, externalReferenceID string) (TopUp, error)
	// MarkFailed closes a top-up not succeeded; ErrTopUpNotFound when there
	// is none (or it succeeded already).
	MarkFailed(ctx context.Context, externalReferenceID, reason string) (TopUp, error)
}

// ConfigReader reads the deployment's wallet config (the top-up limits).
type ConfigReader interface {
	GetActiveConfig(ctx context.Context) (wallet.Config, error)
}
