package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
)

const callTimeout = 5 * time.Second

// Client asks identity-service about the caller WITH THE CALLER'S OWN access
// token, so identity checks that token (and its live session) itself. It never
// uses the internal service token: identity does not accept one.
type Client struct {
	identity identityv1.IdentityServiceClient
}

var _ staff.IdentityReader = (*Client)(nil)

func NewClient(conn grpc.ClientConnInterface) *Client {
	if conn == nil {
		panic("identity-service connection is required")
	}

	return &Client{identity: identityv1.NewIdentityServiceClient(conn)}
}

func (c *Client) Me(ctx context.Context, accessToken string) (staff.VerifiedIdentity, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return staff.VerifiedIdentity{}, errors.New("access token is required")
	}

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+accessToken))

	response, err := c.identity.GetMyIdentity(ctx, &identityv1.GetMyIdentityRequest{})
	if err != nil {
		// A revoked session, or a token identity never issued: the caller is
		// not who the token says, as far as identity is concerned.
		if code := status.Code(err); code == codes.Unauthenticated || code == codes.PermissionDenied {
			return staff.VerifiedIdentity{}, staff.ErrIdentityRejected
		}

		return staff.VerifiedIdentity{}, fmt.Errorf("get my identity: %w", err)
	}

	me := staff.VerifiedIdentity{
		IdentityID: response.GetIdentityId(),
		Active:     response.GetStatus() == identityv1.IdentityStatus_IDENTITY_STATUS_ACTIVE,
	}

	for _, identifier := range response.GetIdentifiers() {
		if identifier.GetType() == identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL {
			me.Emails = append(me.Emails, identifier.GetValue())
		}
	}

	return me, nil
}
