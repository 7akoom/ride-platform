package clients

import (
	"context"
	"fmt"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
)

// identityTimeout bounds each call to identity-service. A PIN check waits on
// a slow hash, so it gets a little more than a lookup would need.
const identityTimeout = 5 * time.Second

// IdentityClient asks identity-service about PINs and phones, as a service
// (the connection carries the internal token).
type IdentityClient struct {
	pins      identityv1.WalletPinServiceClient
	directory identityv1.IdentityDirectoryServiceClient
}

var _ transfer.Identity = (*IdentityClient)(nil)

func NewIdentityClient(identityConn grpc.ClientConnInterface) *IdentityClient {
	if identityConn == nil {
		panic("identity-service connection is required")
	}

	return &IdentityClient{
		pins:      identityv1.NewWalletPinServiceClient(identityConn),
		directory: identityv1.NewIdentityDirectoryServiceClient(identityConn),
	}
}

func (c *IdentityClient) VerifyPIN(ctx context.Context, identityID, pin string) (transfer.PINResult, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	response, err := c.pins.VerifyWalletPin(ctx, &identityv1.VerifyWalletPinRequest{IdentityId: identityID, Pin: pin})
	if err != nil {
		return transfer.PINResult{}, fmt.Errorf("identity-service VerifyWalletPin: %w", err)
	}

	result := transfer.PINResult{AttemptsLeft: int(response.GetAttemptsLeft())}
	if response.GetLockedUntil() != nil {
		result.LockedUntil = response.GetLockedUntil().AsTime()
	}

	switch response.GetResult() {
	case identityv1.PinCheck_PIN_CHECK_OK:
		result.Check = transfer.PINOK
	case identityv1.PinCheck_PIN_CHECK_LOCKED:
		result.Check = transfer.PINLocked
	case identityv1.PinCheck_PIN_CHECK_NOT_SET:
		result.Check = transfer.PINNotSet
	default:
		// Anything else is not a yes.
		result.Check = transfer.PINWrong
	}

	return result, nil
}

func (c *IdentityClient) FindByPhone(ctx context.Context, phone string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	response, err := c.directory.FindIdentityByPhone(ctx, &identityv1.FindIdentityByPhoneRequest{PhoneNumber: phone})

	switch {
	case status.Code(err) == codes.InvalidArgument:
		return "", false, transfer.ErrInvalidPhone
	case err != nil:
		return "", false, fmt.Errorf("identity-service FindIdentityByPhone: %w", err)
	}

	return response.GetIdentityId(), response.GetFound(), nil
}

func (c *IdentityClient) PhoneOf(ctx context.Context, identityID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	response, err := c.directory.GetIdentityPhone(ctx, &identityv1.GetIdentityPhoneRequest{IdentityId: identityID})
	if err != nil {
		return "", fmt.Errorf("identity-service GetIdentityPhone: %w", err)
	}

	return response.GetPhoneNumber(), nil
}
