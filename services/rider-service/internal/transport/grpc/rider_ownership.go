package grpc

import (
	"context"
	"errors"
	"regexp"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
)

const riderRPCPrefix = "/ride.rider.v1.RiderService/"

// RiderReader is the one lookup the owner checks need. rider.Service already
// satisfies it, so ownership is verified against this service's own data.
type RiderReader interface {
	GetRider(ctx context.Context, riderID string) (rider.Rider, error)
}

// caller is the authenticated end user making the request.
type caller struct {
	identityID string
	riders     RiderReader
}

// idShape is the canonical UUID form every profile id has. An id of any other
// shape cannot exist, so it is denied without a lookup.
var idShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ownsIdentity reports whether the request names the caller's own identity.
func (c caller) ownsIdentity(identityID string) bool {
	return c.identityID != "" && identityID == c.identityID
}

// ownsRider reports whether the rider profile exists and belongs to the
// caller. A profile that does not exist counts as not owned, so callers cannot
// tell a missing profile from someone else's.
func (c caller) ownsRider(ctx context.Context, riderID string) (bool, error) {
	if c.identityID == "" || c.riders == nil || !idShape.MatchString(riderID) {
		return false, nil
	}

	found, err := c.riders.GetRider(ctx, riderID)
	if errors.Is(err, rider.ErrRiderNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return found.IdentityID != "" && found.IdentityID == c.identityID, nil
}

var ownerChecks = map[string]ownerCheck{
	riderRPCPrefix + "CreateRider": func(_ context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*riderv1.CreateRiderRequest)
		if !ok {
			return false, nil
		}

		return c.ownsIdentity(r.GetIdentityId()), nil
	},
	riderRPCPrefix + "GetRiderByIdentity": func(_ context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*riderv1.GetRiderByIdentityRequest)
		if !ok {
			return false, nil
		}

		return c.ownsIdentity(r.GetIdentityId()), nil
	},
	riderRPCPrefix + "GetRider": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*riderv1.GetRiderRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	riderRPCPrefix + "UpdateRiderProfile": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*riderv1.UpdateRiderProfileRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
}
